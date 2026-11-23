package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
)

func runtimeStatusFixture() plugin.InstanceStatus {
	now := time.Now().UTC()
	return plugin.InstanceStatus{Manifest: plugin.Manifest{ID: "package", Version: "1.0.0"}, Identity: plugin.Provenance{PackageID: "package", InstanceID: "instance", Version: "1.0.0"}, State: plugin.StateDegraded, LastSuccess: &now, LastFailure: &now, Failure: plugin.FailureTimeout}
}
func exercisePluginJournal(t *testing.T, j plugin.RuntimeJournal) {
	t.Helper()
	ctx := context.Background()
	s := runtimeStatusFixture()
	if err := j.SavePluginStatus(ctx, s); err != nil {
		t.Fatal(err)
	}
	rows, err := j.LoadPluginStatuses(ctx)
	if err != nil || len(rows) != 1 || rows[0].Failure != plugin.FailureTimeout || rows[0].LastSuccess == nil {
		t.Fatalf("journal failed: %+v %v", rows, err)
	}
	rows[0].Identity.PackageID = "modified"
	loaded, err := j.LoadPluginStatuses(ctx)
	if err != nil || loaded[0].Identity.PackageID != "package" {
		t.Fatal("journal exposed mutable state")
	}
	s.Failure = "raw-token-secret"
	if j.SavePluginStatus(ctx, s) == nil {
		t.Fatal("persisted unclassified diagnostic")
	}
}
func TestPluginRuntimeFileJournalSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	exercisePluginJournal(t, st)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.LoadPluginStatuses(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Failure != plugin.FailureTimeout {
		t.Fatal(rows, err)
	}
}
func TestPluginRuntimePostgresJournal(t *testing.T) {
	url := postgresTestURL(t)
	st, err := OpenPostgres(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	exercisePluginJournal(t, st)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenPostgres(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.LoadPluginStatuses(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
