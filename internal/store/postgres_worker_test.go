package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/jackc/pgx/v5"
)

func postgresWorkerFixture(t *testing.T) (*Postgres, string, Job) {
	t.Helper()
	ctx := context.Background()
	url := postgresTestURL(t)
	st, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	res, err := st.UpsertResource(ctx, completeResource("worker-resource"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.CreateRecommendation(ctx, Recommendation{ResourceID: res.ID, PluginID: "plugin", ActionType: "resize", AlgorithmID: "algorithm", EvidenceRefs: []string{"evidence:1"}})
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.CreateAction(ctx, Action{RecommendationID: rec.ID, ResourceID: res.ID, RemediationPath: "direct_apply", PluginID: "plugin", ActionType: "resize", Mode: "approved", IdempotencyKey: "worker-test", RequestedBy: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.CreateJob(ctx, Job{ID: rec.ID, ActionID: action.ID, RecommendationID: rec.ID, Resource: res, Plan: plugin.ActionPlan{PluginID: "plugin", ResourceID: res.ID, ActionType: "resize", OriginalState: map[string]any{"size": 100}, AppliedState: map[string]any{"size": 75}}, Actor: "operator", Deadline: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return st, url, job
}

func TestPostgresWorkerClaimRaceAndExpiredOwnerFencing(t *testing.T) {
	st, url, job := postgresWorkerFixture(t)
	ctx := context.Background()
	other, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wins atomic.Int32
	var winner Job
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := st
			if i%2 == 1 {
				p = other
			}
			got, err := p.ClaimJob(ctx, job, fmt.Sprintf("worker-%d", i), time.Minute)
			if err == nil {
				wins.Add(1)
				mu.Lock()
				winner = got
				mu.Unlock()
			} else if !errors.Is(err, ErrJobBusy) && !errors.Is(err, ErrJobChanged) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners=%d", wins.Load())
	}
	if err := other.RenewJobLease(ctx, winner, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE jobs SET payload=jsonb_set(payload,'{lease,until}',to_jsonb(clock_timestamp()-interval '1 second')) WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	jobs, err := other.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	takeover, err := other.ClaimJob(ctx, jobs[0], "replacement", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if takeover.Fence <= winner.Fence || takeover.History[0].Outcome != "lease_expired" {
		t.Fatalf("takeover=%+v", takeover)
	}
	winner.State = "applying"
	if err := st.SaveJob(ctx, winner, "stale dispatch"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale write=%v", err)
	}
	if err := st.ReleaseJobLease(ctx, winner, "completed"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale release=%v", err)
	}
	if err := other.ReleaseJobLease(ctx, takeover, "interrupted"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.CancelJob(ctx, job.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE jobs SET payload=payload-'version' WHERE id=$1`, job.ID); err == nil {
		t.Fatal("old worker stripped fencing metadata")
	}
}

func TestPostgresWorkerClaimAndAuditAreAtomic(t *testing.T) {
	st, _, job := postgresWorkerFixture(t)
	ctx := context.Background()
	if _, err := st.pool.Exec(ctx, `ALTER TABLE audit_events ADD CONSTRAINT worker_test_audit_failure CHECK(false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimJob(ctx, job, "worker", time.Minute); err == nil {
		t.Fatal("claim succeeded without durable audit")
	}
	jobs, err := st.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].Lease != nil || len(jobs[0].History) != 0 || jobs[0].Version != job.Version {
		t.Fatalf("failed claim partially committed: %+v", jobs[0])
	}
}

func TestPostgresWorkerTwoProcessesDispatchOneIntent(t *testing.T) {
	st, url, _ := postgresWorkerFixture(t)
	ctx := context.Background()
	if _, err := st.pool.Exec(ctx, `CREATE TABLE worker_test_effects(count integer NOT NULL); INSERT INTO worker_test_effects VALUES(0)`); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	children := make([]*exec.Cmd, 2)
	for i := range children {
		children[i] = exec.Command(binary, "-test.run=^TestPostgresWorkerChild$", "-test.timeout=30s")
		children[i].Env = append(os.Environ(), "CONSIZE_WORKER_CHILD="+strconv.Itoa(i+1), "CONSIZE_TEST_DATABASE_URL="+url)
		if err := children[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, child := range children {
		if err := child.Wait(); err != nil {
			t.Fatalf("worker subprocess failed: %v", err)
		}
	}
	var count int
	if err := st.pool.QueryRow(ctx, `SELECT count FROM worker_test_effects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("external effect count=%d, want exactly one", count)
	}
	jobs, _ := st.ListJobs(ctx)
	if jobs[0].State != "applying" {
		t.Fatalf("intent not durable: %+v", jobs[0])
	}
}

func TestPostgresWorkerChild(t *testing.T) {
	owner := os.Getenv("CONSIZE_WORKER_CHILD")
	if owner == "" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	url := os.Getenv("CONSIZE_TEST_DATABASE_URL")
	st, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	jobs, err := st.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].State != "prepared" {
		return
	}
	job, err := st.ClaimJob(ctx, jobs[0], "process-"+owner, time.Minute)
	if errors.Is(err, ErrJobBusy) || errors.Is(err, ErrJobChanged) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	job.State = "applying"
	if err := st.SaveJob(ctx, job, "dispatch intent persisted"); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE worker_test_effects SET count=count+1`); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseJobLease(ctx, job, "completed"); err != nil {
		t.Fatal(err)
	}
}

// Bounded contention soak: all workers repeatedly renew and finish claims against the same row.
func TestPostgresWorkerContentionSoak(t *testing.T) {
	st, url, _ := postgresWorkerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	peers := []*Postgres{st}
	for i := 0; i < 3; i++ {
		p, err := OpenPostgres(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		peers = append(peers, p)
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	for i, p := range peers {
		wg.Add(1)
		go func(i int, p *Postgres) {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				jobs, err := p.ListJobs(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				job, err := p.ClaimJob(ctx, jobs[0], fmt.Sprintf("soak-%d", i), time.Minute)
				if errors.Is(err, ErrJobBusy) || errors.Is(err, ErrJobChanged) {
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				if err := p.RenewJobLease(ctx, job, time.Minute); err != nil {
					t.Error(err)
					return
				}
				if err := p.ReleaseJobLease(ctx, job, "completed"); err != nil {
					t.Error(err)
					return
				}
				completed.Add(1)
			}
		}(i, p)
	}
	wg.Wait()
	jobs, err := st.ListJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Load() < 1 || len(jobs[0].History) != int(completed.Load()) || jobs[0].Lease != nil {
		t.Fatalf("inconsistent soak completion: %d %+v", completed.Load(), jobs[0])
	}
	for i, a := range jobs[0].History {
		if a.Token != uint64(i+1) || a.Outcome != "completed" || a.FinishedAt.IsZero() {
			t.Fatalf("lost/repeated fence or attempt: %+v", a)
		}
	}
	t.Logf("%d fenced claims completed across four independent PostgreSQL pools", completed.Load())
}
