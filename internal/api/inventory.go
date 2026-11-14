package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/inventory"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/internal/verification"
	"github.com/consize-oss/consize/pkg/resource"
)

const inventoryCursorTTL = 5 * time.Minute

type inventoryCursor struct {
	At     time.Time `json:"at"`
	After  string    `json:"after"`
	Digest string    `json:"digest"`
	Filter string    `json:"filter"`
}
type inventoryPage struct {
	Resources         []inventory.Row   `json:"resources"`
	Summary           inventory.Summary `json:"summary"`
	FilteredTotal     int               `json:"filtered_total"`
	AssessmentVersion string            `json:"assessment_version"`
	AssessedAt        time.Time         `json:"assessed_at"`
	SnapshotID        string            `json:"snapshot_id"`
	NextCursor        string            `json:"next_cursor"`
	HasMore           bool              `json:"has_more"`
}

func (s *Server) signInventoryCursor(c inventoryCursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.inventoryKey[:])
	_, _ = mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) readInventoryCursor(value string) (inventoryCursor, error) {
	var c inventoryCursor
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return c, resource.ErrInvalidQuery
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, resource.ErrInvalidQuery
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, resource.ErrInvalidQuery
	}
	mac := hmac.New(sha256.New, s.inventoryKey[:])
	_, _ = mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(b, &c) != nil || c.After == "" || c.At.IsZero() {
		return c, resource.ErrInvalidQuery
	}
	return c, nil
}

func digestInventory(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(r.URL.RawQuery) > 16384 {
		writeError(w, http.StatusBadRequest, "inventory query too large")
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid inventory query encoding")
		return
	}
	q, err := inventory.ParseQuery(values)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := s.inventoryNow().UTC()
	filter := digestInventory(q)
	cursor := inventoryCursor{At: now, Filter: filter}
	if q.Cursor != "" {
		cursor, err = s.readInventoryCursor(q.Cursor)
		if err != nil || cursor.Filter != filter || cursor.At.After(now) {
			writeError(w, http.StatusBadRequest, "invalid inventory cursor for these filters")
			return
		}
		if now.Sub(cursor.At) > inventoryCursorTTL {
			writeError(w, http.StatusConflict, "inventory snapshot expired; restart from the first page")
			return
		}
	}
	reader, ok := s.st.(store.InventoryReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "inventory assessment unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	input, err := reader.ReadInventory(ctx)
	if err != nil {
		message := "inventory assessment unavailable"
		if errors.Is(err, store.ErrInventoryLimit) {
			message = "inventory exceeds assessment safety budget"
		}
		// Driver details and persisted plugin payloads may contain secrets.
		writeError(w, http.StatusServiceUnavailable, message)
		return
	}
	assessed, err := inventory.Build(ctx, input, s.plugins.Manifests(), s.policies, s.inventorySignals(), cursor.At)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory assessment timed out")
		return
	}
	rows, summary := inventory.Filter(assessed, q)
	if err = ctx.Err(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "inventory assessment timed out")
		return
	}
	digest := digestInventory(struct {
		Rows    []inventory.Row
		Summary inventory.Summary
	}{rows, summary})
	if q.Cursor != "" && digest != cursor.Digest {
		writeError(w, http.StatusConflict, "inventory changed; restart from the first page")
		return
	}
	start := 0
	if cursor.After != "" {
		start = sort.Search(len(rows), func(i int) bool { return rows[i].Resource.ID >= cursor.After })
		if start == len(rows) || rows[start].Resource.ID != cursor.After {
			writeError(w, http.StatusBadRequest, "invalid inventory cursor position")
			return
		}
		start++
	}
	end := start + q.Limit
	if end > len(rows) {
		end = len(rows)
	}
	page := inventoryPage{Resources: rows[start:end], Summary: summary, FilteredTotal: len(rows), AssessmentVersion: inventory.Version, AssessedAt: cursor.At, SnapshotID: digest, HasMore: end < len(rows)}
	if page.HasMore {
		cursor.Digest, cursor.After = digest, rows[end-1].Resource.ID
		page.NextCursor = s.signInventoryCursor(cursor)
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) inventorySignals() []inventory.Signal {
	// Reuse the existing verification contract: default CPU/memory/restart
	// measurements plus configured safety checks. No UI-defined quality rules.
	units := map[string]string{"memory_working_set_bytes": "bytes", "cpu_cores": "cores", "restarts_30m": "count", "cpu_throttling_ratio": "ratio", "error_rate": "ratio", "latency_seconds": "seconds"}
	checks := map[string]string{}
	for _, c := range s.cfg.Verification.Checks {
		checks[c.Signal] = c.Statistic
		if c.Unit != "" {
			units[c.Signal] = c.Unit
		}
	}
	out := []inventory.Signal{}
	for _, key := range verification.RequiredSignals(s.cfg.Verification) {
		stat := "p95"
		if key == "restarts_30m" {
			stat = "max"
		}
		if checks[key] != "" {
			stat = checks[key]
		}
		unit := units[key]
		if unit == "" {
			unit = "count"
		}
		out = append(out, inventory.Signal{Name: key + "_" + stat, Unit: unit})
	}
	return out
}
