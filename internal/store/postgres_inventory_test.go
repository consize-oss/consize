package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Uses the same isolated-schema PostgreSQL harness as the registry/worker
// suites. CI provisions the pinned PostgreSQL image automatically.
func TestPostgresInventorySnapshotDuringAtomicRefresh(t *testing.T) {
	ctx := context.Background()
	st, err := OpenPostgres(ctx, postgresTestURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	res, err := st.UpsertResource(ctx, completeResource("inventory-resource"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateRecommendation(ctx, Recommendation{ResourceID: res.ID, PluginID: "plugin", ActionType: "resize", AlgorithmID: "test", EvidenceRefs: []string{"legacy:fixture"}, Title: res.Owner})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := st.ReadInventory(ctx)
	if err != nil || len(initial.Resources) != 1 || len(initial.Recommendations) != 1 || initial.Resources[0].Owner != initial.Recommendations[0].Title {
		t.Fatalf("initial snapshot: %+v %v", initial, err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			tx, err := st.pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				failures <- err
				return
			}
			_, err = tx.Exec(ctx, `UPDATE resources SET payload=jsonb_set(payload,'{owner}',to_jsonb($1::text))`, fmt.Sprint(i))
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE recommendations SET payload=jsonb_set(payload,'{title}',to_jsonb($1::text))`, fmt.Sprint(i))
			}
			if err != nil {
				_ = tx.Rollback(ctx)
				failures <- err
				return
			}
			if err = tx.Commit(ctx); err != nil {
				failures <- err
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		snapshot, err := st.ReadInventory(ctx)
		if err != nil {
			t.Error(err)
			break
		}
		if snapshot.Resources[0].Owner != snapshot.Recommendations[0].Title {
			t.Error("inventory mixed rows from different committed database snapshots")
			break
		}
	}
	wg.Wait()
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = st.ReadInventory(cancelled); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}
