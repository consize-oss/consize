package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInventorySnapshotIsDetachedAtomicAndCancellable(t *testing.T) {
	m := NewMemory()
	m.resources["a"] = completeResource("a")
	m.recs[1] = Recommendation{ID: 1, ResourceID: "a", Title: "initial"}
	first, err := m.ReadInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.Resources[0].Metadata["tamper"] = "changed"
	first.Recommendations[0].Title = "changed"
	second, err := m.ReadInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Resources[0].Metadata["tamper"] != nil || second.Recommendations[0].Title != "initial" {
		t.Fatal("snapshot aliases persisted state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.ReadInventory(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	m.mu.Lock()
	r := m.resources["a"]
	r.Owner = "0"
	m.resources["a"] = r
	rec := m.recs[1]
	rec.Title = "0"
	m.recs[1] = rec
	m.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			m.mu.Lock()
			r := m.resources["a"]
			r.Owner = fmt.Sprint(i)
			m.resources["a"] = r
			rec := m.recs[1]
			rec.Title = r.Owner
			m.recs[1] = rec
			m.mu.Unlock()
		}
	}()
	for i := 0; i < 100; i++ {
		snap, err := m.ReadInventory(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if snap.Resources[0].Owner != snap.Recommendations[0].Title {
			t.Fatal("torn snapshot")
		}
	}
	wg.Wait()
}
func TestInventorySafetyBudgetsFailWithoutPartialResults(t *testing.T) {
	remaining := 2
	var out []Recommendation
	if err := decodeInventory(&out, []byte(`{"id":1}`), &remaining); !errors.Is(err, ErrInventoryLimit) || len(out) != 0 {
		t.Fatal("byte budget did not fail closed")
	}
	m := NewMemory()
	for i := 0; i <= inventoryEntityLimit; i++ {
		m.recs[int64(i)] = Recommendation{ID: int64(i)}
	}
	if snap, err := m.ReadInventory(context.Background()); !errors.Is(err, ErrInventoryLimit) || len(snap.Recommendations) != 0 {
		t.Fatal("entity budget returned partial totals")
	}
	m = NewMemory()
	m.poison = errors.New("persist failed")
	if _, err := m.ReadInventory(context.Background()); err == nil {
		t.Fatal("poisoned store served inventory")
	}
}
