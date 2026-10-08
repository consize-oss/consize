package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
)

func TestFreshInstallCreatesCurrentSchemaAndDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	before, err := InspectDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.MigrationStatus != MigrationPending || before.CurrentVersion != 0 {
		t.Fatalf("unexpected pre-install diagnostics: %#v", before)
	}
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := st.StorageDiagnostics(context.Background())
	if diagnostics.CurrentVersion != currentStateVersion || diagnostics.MigrationStatus != MigrationCurrent || !diagnostics.Healthy || !diagnostics.Durable {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if len(diagnostics.Indexes) != len(RequiredIndexes()) {
		t.Fatalf("indexes = %v", diagnostics.Indexes)
	}
	st.Close()
}

func TestUpgradeFromV4PreservesIdentifiersAndRelationships(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, rec, action, job := persistedWorkflow(t, path)
	st.Close()
	state := readDiskState(t, path)
	state.Version = 4
	state.Migrations = nil
	writeDiskState(t, path, state)

	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gotRec, err := st.GetRecommendation(context.Background(), rec.ID)
	if err != nil || gotRec.ResourceID != rec.ResourceID {
		t.Fatalf("recommendation was not preserved: %#v %v", gotRec, err)
	}
	gotAction, err := st.GetAction(context.Background(), action.ID)
	if err != nil || gotAction.RecommendationID != rec.ID {
		t.Fatalf("action was not preserved: %#v %v", gotAction, err)
	}
	jobs, err := st.ListJobs(context.Background())
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].ActionID != action.ID {
		t.Fatalf("job was not preserved: %#v %v", jobs, err)
	}
	diagnostics := st.StorageDiagnostics(context.Background())
	if diagnostics.LoadedVersion != 4 || diagnostics.CurrentVersion != currentStateVersion || diagnostics.MigrationStatus != MigrationApplied {
		t.Fatalf("migration status = %#v", diagnostics)
	}
}

func TestUnsupportedSchemaFailsWithRemediation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	writeDiskState(t, path, diskState{Version: currentStateVersion + 1})
	_, err := OpenDurable(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported state schema version") || !strings.Contains(err.Error(), "storage status") {
		t.Fatalf("error = %v", err)
	}
}

func TestDuplicateIdempotencyIndexFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, _, action, _ := persistedWorkflow(t, path)
	st.Close()
	state := readDiskState(t, path)
	duplicate := action
	duplicate.ID = state.NextActionID
	state.ActionRecords[duplicate.ID] = duplicate
	state.NextActionID++
	writeDiskState(t, path, state)
	if st, err := OpenDurable(path); err == nil {
		st.Close()
		t.Fatal("duplicate unique index value was accepted")
	}
}

func TestInterruptedWorkflowSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, rec, action, job := persistedWorkflow(t, path)
	if err := st.SaveJob(context.Background(), job, "pending restart"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	jobs, err := st.ListJobs(context.Background())
	if err != nil || len(jobs) != 1 || jobs[0].RecommendationID != rec.ID || jobs[0].ActionID != action.ID || jobs[0].State != "prepared" {
		t.Fatalf("pending workflow did not survive: %#v %v", jobs, err)
	}
}

func TestFailedPersistenceLeavesLastCommittedFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, _, _, _ := persistedWorkflow(t, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st.path = filepath.Join(dir, "missing", "state.json")
	if err := st.TransitionRecommendation(context.Background(), 1, RecommendationRejected, "inject write failure"); err == nil {
		t.Fatal("write failure was ignored")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed transaction changed the last committed state file")
	}
	st.Close()
}

func TestPartialMigrationFailureDoesNotRewriteSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, _, _, _ := persistedWorkflow(t, path)
	st.Close()
	state := readDiskState(t, path)
	state.Version = 4
	state.Migrations = nil
	for id, rec := range state.Recommendations {
		rec.ResourceID = "missing"
		state.Recommendations[id] = rec
	}
	writeDiskState(t, path, state)
	before, _ := os.ReadFile(path)
	if st, err := OpenDurable(path); err == nil {
		st.Close()
		t.Fatal("invalid migrated state was accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed migration rewrote the source state")
	}
}

func TestBackupRestoreAndChecksumValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	backup := filepath.Join(dir, "backups", "state.backup.json")
	st, rec, _, _ := persistedWorkflow(t, path)
	st.Close()
	if _, err := BackupDurable(path, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetDurable(path, "wrong"); err == nil {
		t.Fatal("reset without confirmation succeeded")
	}
	archive, err := ResetDurable(path, "ERASE LOCAL STATE")
	if err != nil || archive == "" {
		t.Fatalf("reset archive: %q %v", archive, err)
	}
	if _, err := RestoreDurable(path, backup); err != nil {
		t.Fatal(err)
	}
	st, err = OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRecommendation(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err := os.WriteFile(backup, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreDurable(path, backup); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered backup error = %v", err)
	}
}

func persistedWorkflow(t *testing.T, path string) (*Memory, Recommendation, Action, Job) {
	t.Helper()
	st, err := OpenDurable(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.UpsertResource(context.Background(), completeResource("resource-1"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.CreateRecommendation(context.Background(), Recommendation{ResourceID: res.ID, PluginID: "plugin", ActionType: "resize", AlgorithmID: "algorithm", AlgorithmVersion: "1", EvidenceRefs: []string{"evidence:1"}, Confidence: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.CreateAction(context.Background(), Action{RecommendationID: rec.ID, ResourceID: res.ID, RemediationPath: "direct_apply", PluginID: "plugin", PluginVersion: "1", ActionType: "resize", Mode: "approved", IdempotencyKey: "workflow-1", RequestedBy: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: rec.ID, ActionID: action.ID, RecommendationID: rec.ID, Resource: res, Plan: plugin.ActionPlan{PluginID: "plugin", ResourceID: res.ID, ActionType: "resize", Summary: "resize", Diff: map[string]any{}, OriginalState: map[string]any{"size": 10}, AppliedState: map[string]any{"size": 8}}, Actor: "operator", Deadline: time.Now().Add(time.Hour)}
	job, err = st.CreateJob(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	return st, rec, action, job
}

func readDiskState(t *testing.T, path string) diskState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeDiskState(data)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func writeDiskState(t *testing.T, path string, state diskState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
