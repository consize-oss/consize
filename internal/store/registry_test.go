package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/resource"
)

func registryScope(r resource.Resource) resource.Scope {
	return resource.Scope{Provider: r.Provider, Account: r.Account, Region: r.Region, Type: r.Type, SourcePluginID: r.SourcePluginID}
}

func exerciseRegistry(t *testing.T, st resource.Registry) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour)
	input := resourceInput(now)
	input.SourcePluginID = "discovery-one"
	created, err := st.CreateResource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.CreateResource(ctx, input); !errors.Is(err, resource.ErrAlreadyExists) {
		t.Fatal("create duplicate", err)
	}
	update := input
	update.ObservedAt = now.Add(time.Minute)
	update.Owner = "new-owner"
	update.FirstSeenAt = now.Add(-24 * time.Hour)
	got, err := st.UpsertResource(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || !got.FirstSeenAt.Equal(created.FirstSeenAt) || got.Owner != "new-owner" || !got.LastSeenAt.Equal(update.ObservedAt) {
		t.Fatal("upsert semantics", got)
	}
	if _, err = st.UpsertResource(ctx, input); !errors.Is(err, resource.ErrStaleObservation) {
		t.Fatal("stale observation", err)
	}
	other := input
	other.ProviderResourceID = "other"
	other.SourcePluginID = "discovery-two"
	outside, err := st.UpsertResource(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	scope := registryScope(created)
	if _, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedAt: now.Add(2 * time.Minute), ObservedIDs: []string{outside.ID}}); !errors.Is(err, resource.ErrScopeMismatch) {
		t.Fatal("cross-source reconciliation accepted", err)
	}
	unchanged, _ := st.GetResource(ctx, created.ID)
	if unchanged.LifecycleState != resource.LifecycleActive {
		t.Fatal("failed reconciliation partially persisted")
	}
	result, err := st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedAt: now.Add(2 * time.Minute)})
	if err != nil || len(result.Stale) != 1 {
		t.Fatal("missing resource not stale", err)
	}
	stale, _ := st.GetResource(ctx, created.ID)
	if stale.LifecycleState != resource.LifecycleStale || !stale.LastSeenAt.Equal(got.LastSeenAt) {
		t.Fatal("stale fabricated observation", stale)
	}
	outside, _ = st.GetResource(ctx, outside.ID)
	if outside.LifecycleState != resource.LifecycleActive {
		t.Fatal("other scope altered")
	}
	result, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedAt: now.Add(3 * time.Minute), ConfirmedDeletedIDs: []string{created.ID}})
	if err != nil || len(result.Deleted) != 1 {
		t.Fatal("confirmed tombstone failed", err)
	}
	update.ObservedAt = now.Add(2 * time.Minute)
	if _, err = st.UpsertResource(ctx, update); !errors.Is(err, resource.ErrStaleObservation) {
		t.Fatal("old observation resurrected tombstone", err)
	}
	update.ObservedAt = now.Add(4 * time.Minute)
	got, err = st.UpsertResource(ctx, update)
	if err != nil || got.LifecycleState != resource.LifecycleActive || !got.FirstSeenAt.Equal(created.FirstSeenAt) {
		t.Fatal("reappearance failed", err)
	}
	if _, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedAt: now.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetResource(ctx, created.ID)
	if got.LifecycleState != resource.LifecycleActive {
		t.Fatal("delayed scan overrode newer observation")
	}
	if _, err = st.UpdateResourceLifecycle(ctx, resource.LifecycleUpdate{Scope: scope, ID: created.ID, State: resource.LifecycleDeleted, ObservedAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err = st.UpdateResourceLifecycle(ctx, resource.LifecycleUpdate{Scope: scope, ID: created.ID, State: resource.LifecycleStale, ObservedAt: now.Add(6 * time.Minute)}); !errors.Is(err, resource.ErrInvalidTransition) {
		t.Fatal("invalid lifecycle accepted", err)
	}
	if _, err = st.GetResource(ctx, "missing"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatal("not found not typed", err)
	}
}

func TestDurableRegistryContract(t *testing.T) {
	st, err := OpenDurable(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exerciseRegistry(t, st)
}

func exerciseRegistryPagination(t *testing.T, st resource.Registry) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"c", "a", "b"} {
		r := resourceInput(now)
		r.ID = id
		r.Owner = "paging-owner"
		r.ProviderResourceID = id
		if _, err := st.UpsertResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	first, err := st.ListResourcePage(ctx, resource.Query{Provider: "kubernetes", Owner: "paging-owner", Limit: 2})
	if err != nil || len(first.Resources) != 2 || first.Resources[0].ID != "a" || !first.HasMore {
		t.Fatal("first page", first, err)
	}
	last, err := st.ListResourcePage(ctx, resource.Query{Provider: "kubernetes", Owner: "paging-owner", Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(last.Resources) != 1 || last.Resources[0].ID != "c" || last.HasMore {
		t.Fatal("last page", last, err)
	}
	if _, err = st.ListResourcePage(ctx, resource.Query{Provider: "aws", Cursor: first.NextCursor}); !errors.Is(err, resource.ErrInvalidQuery) {
		t.Fatal("cursor scope mismatch accepted", err)
	}
	if _, err = st.ListResourcePage(ctx, resource.Query{Cursor: "malformed"}); !errors.Is(err, resource.ErrInvalidQuery) {
		t.Fatal("bad cursor accepted", err)
	}
}

func TestRegistryPagination(t *testing.T) { exerciseRegistryPagination(t, NewMemory()) }

func exerciseConcurrentRegistry(t *testing.T, st resource.Registry) {
	t.Helper()
	ctx := context.Background()
	input := resourceInput(time.Now().UTC())
	input.ProviderResourceID = "concurrent-resource"
	input.SourcePluginID = "concurrent-source"
	var wg sync.WaitGroup
	created := make(chan resource.Resource, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := st.CreateResource(ctx, input)
			if err == nil {
				created <- r
			} else if !errors.Is(err, resource.ErrAlreadyExists) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(created)
	if len(created) != 1 {
		t.Fatalf("concurrent create winners = %d", len(created))
	}
	first := <-created
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.UpsertResource(ctx, input); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := st.GetResource(ctx, first.ID)
	if err != nil || !got.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Fatal("concurrent update changed identity/first seen", err)
	}
}
func TestConcurrentRegistry(t *testing.T) { exerciseConcurrentRegistry(t, NewMemory()) }

func TestRegistryLifecyclePreservesHistoryAcrossRestart(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "registry.json")
	st, err := OpenDurable(file)
	if err != nil {
		t.Fatal(err)
	}
	input := resourceInput(time.Now().Add(-time.Hour))
	input.SourcePluginID = "history-source"
	r, err := st.UpsertResource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.CreateRecommendation(ctx, evidenceRecommendation(t, r.ID, 100))
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.CreateAction(ctx, Action{RecommendationID: rec.ID, ResourceID: r.ID, PluginID: rec.PluginID, ActionType: rec.ActionType, Mode: "approved", RequestedBy: "test", RemediationPath: "direct_apply", IdempotencyKey: "registry-history"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.UpdateResourceLifecycle(ctx, resource.LifecycleUpdate{Scope: registryScope(r), ID: r.ID, State: resource.LifecycleDeleted, ObservedAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = OpenDurable(file)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	restored, err := st.GetResource(ctx, r.ID)
	if err != nil || restored.LifecycleState != resource.LifecycleDeleted || restored.LifecycleObservedAt.IsZero() {
		t.Fatal("tombstone not restored", err)
	}
	if _, err = st.GetRecommendation(ctx, rec.ID); err != nil {
		t.Fatal("recommendation lost", err)
	}
	if _, err = st.GetAction(ctx, action.ID); err != nil {
		t.Fatal("action lost", err)
	}
}

func TestReconciliationPresenceRejectsOlderMissingScan(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	now := time.Now().Add(-time.Hour)
	input := resourceInput(now)
	input.SourcePluginID = "source"
	r, err := st.UpsertResource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	scope := registryScope(r)
	if _, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedIDs: []string{r.ID}, ObservedAt: now.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: scope, ObservedAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetResource(ctx, r.ID)
	if got.LifecycleState != resource.LifecycleActive || !got.LastSeenAt.Equal(now.Add(3*time.Minute)) {
		t.Fatal("delayed scan changed presence", got)
	}
}

func TestRegistryReconciliationAllScopeCoordinates(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	now := time.Now().Add(-time.Hour)
	input := resourceInput(now)
	input.SourcePluginID = "scope-source"
	target, err := st.UpsertResource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*resource.Resource){func(r *resource.Resource) { r.Provider = "other" }, func(r *resource.Resource) { r.Account = "other" }, func(r *resource.Resource) { r.Region = "other" }, func(r *resource.Resource) { r.Type = "other.type" }, func(r *resource.Resource) { r.SourcePluginID = "other"; r.ProviderResourceID = "other-source" }} {
		r := input
		change(&r)
		if _, err = st.UpsertResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	result, err := st.ReconcileResources(ctx, resource.Reconciliation{Scope: registryScope(target), ObservedAt: now.Add(time.Minute)})
	if err != nil || len(result.Stale) != 1 || result.Stale[0] != target.ID {
		t.Fatal("scope escaped", result, err)
	}
	rows, _ := st.ListResources(ctx)
	for _, r := range rows {
		if r.ID != target.ID && r.LifecycleState != resource.LifecycleActive {
			t.Fatal("unrelated scope changed", r.ID)
		}
	}
}

func TestRegistryPersistenceFailureRollsBackWrites(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	input := resourceInput(time.Now().Add(-time.Hour))
	input.SourcePluginID = "failure-source"
	original, err := st.UpsertResource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	st.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if _, err = st.ReconcileResources(ctx, resource.Reconciliation{Scope: registryScope(original), ObservedAt: time.Now().Add(-time.Minute)}); err == nil {
		t.Fatal("failed reconciliation published")
	}
	got, _ := st.GetResource(ctx, original.ID)
	if got.LifecycleState != resource.LifecycleActive || !got.LastSeenAt.Equal(original.LastSeenAt) {
		t.Fatal("failed reconciliation changed memory")
	}
	if _, err = st.CreateResource(ctx, func() resource.Resource { r := input; r.ProviderResourceID = "new-failed"; return r }()); err == nil {
		t.Fatal("failed create published")
	}
	rows, _ := st.ListResources(ctx)
	if len(rows) != 1 {
		t.Fatal("failed create retained identity")
	}
}

func TestRegistryCanceledWrites(t *testing.T) {
	st := NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.UpsertResource(ctx, resourceInput(time.Now())); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled write accepted", err)
	}
}

func TestRegistryReadModifyWriteIgnoresServerClocks(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	r, err := st.UpsertResource(ctx, resourceInput(time.Now().Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	first := r.FirstSeenAt
	r.ObservedAt = r.ObservedAt.Add(time.Minute)
	r.Owner = "changed"
	updated, err := st.UpsertResource(ctx, r)
	if err != nil || !updated.FirstSeenAt.Equal(first) || !updated.LastSeenAt.Equal(r.ObservedAt) {
		t.Fatal("read/modify/write retained stale server clocks", err)
	}
}
