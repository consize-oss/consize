package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/internal/store"
)

func TestWorkerCancellationAPIIsAtomicAndHonest(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "claimed"}[claimed], func(t *testing.T) {
			server, st, client := apiFixture(t)
			request := func(method, path, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				server.Handler().ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
				return w
			}
			if w := request(http.MethodPost, "/api/recommendations/1/execute", `{"mode":"approved"}`); w.Code != 202 {
				t.Fatalf("submit: %d %s", w.Code, w.Body)
			}
			if claimed {
				jobs, _ := st.ListJobs(context.Background())
				if _, err := st.ClaimJob(context.Background(), jobs[0], "worker-test", time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			w := request(http.MethodPost, "/api/recommendations/1/cancel", "")
			want := 200
			if claimed {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("cancel: got %d want %d: %s", w.Code, want, w.Body)
			}
			if !claimed && request(http.MethodPost, "/api/recommendations/1/cancel", "").Code != 200 {
				t.Fatal("repeated cancel not idempotent")
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing cancellation correlation")
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" {
					t.Fatal("cancel mutated provider")
				}
			}
			w = request(http.MethodGet, "/api/jobs", "")
			var response struct {
				Jobs   []store.Job         `json:"jobs"`
				Worker store.WorkerSummary `json:"worker"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if claimed && response.Worker.ActiveLeases != 1 {
				t.Fatalf("worker summary: %+v", response.Worker)
			}
			if !claimed && (response.Jobs[0].State != "cancelled" || response.Worker.Completed != 1) {
				t.Fatalf("cancel status: %+v", response)
			}
		})
	}
}
