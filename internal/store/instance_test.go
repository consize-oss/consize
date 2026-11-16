package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

func exerciseInstanceGuard(t *testing.T, st resource.Registry) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour)
	in := resourceInput(now)
	in.Metadata = map[string]any{"uid": "fixture-original"}
	created, err := st.UpsertResource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// A legacy record adopts the new versioned mapper extension without ID changes.
	update := resourceInput(now.Add(time.Minute))
	update.Metadata = map[string]any{"uid": "fixture-original", "provider_instance_id": "fixture-original", "mapping": map[string]any{"version": "kubernetes-deployment-mapper-v1"}}
	saved, err := st.UpsertResource(ctx, update)
	if err != nil || saved.ID != created.ID {
		t.Fatal("legacy mapping upgrade", err)
	}
	for _, metadata := range []map[string]any{
		{"uid": "fixture-replacement", "provider_instance_id": "fixture-replacement"},
		{},
		{"uid": "fixture-original", "provider_instance_id": "fixture-replacement"},
		{"uid": "fixture-replacement", "provider_instance_id": "fixture-original"},
	} {
		bad := resourceInput(now.Add(2 * time.Minute))
		bad.Metadata = metadata
		if _, err = st.UpsertResource(ctx, bad); !errors.Is(err, resource.ErrIdentityConflict) && !errors.Is(err, resource.ErrInvalidResource) {
			t.Fatal("replacement or downgrade accepted", err)
		}
		got, err := st.GetResource(ctx, saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(saved)
		b, _ := json.Marshal(got)
		if string(a) != string(b) {
			t.Fatal("failed replacement changed stored resource")
		}
	}
	unknown := resourceInput(now)
	unknown.ProviderResourceID = "payments/unknown"
	unknown.Name = "unknown"
	unknown.Environment = resource.EnvUnknown
	if _, err = st.UpsertResource(ctx, unknown); err != nil {
		t.Fatal(err)
	}
	page, err := st.ListResourcePage(ctx, resource.Query{Environment: resource.EnvUnknown})
	if err != nil || len(page.Resources) != 1 {
		t.Fatal("unknown environment filter", err)
	}
}
func TestMemoryProviderInstanceGuard(t *testing.T) { exerciseInstanceGuard(t, NewMemory()) }
func TestDurableProviderInstanceGuard(t *testing.T) {
	st, err := OpenDurable(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exerciseInstanceGuard(t, st)
}
func TestPostgresProviderInstanceGuard(t *testing.T) {
	st, err := OpenPostgres(context.Background(), postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exerciseInstanceGuard(t, st)
}
