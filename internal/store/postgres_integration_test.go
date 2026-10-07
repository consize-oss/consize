package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/jackc/pgx/v5"
)

const postgresTestImage = "postgres:17.6-alpine@sha256:ef257d85f76e48da1c64832459b59fcaba1a4dac97bf5d7450c77753542eee94"

func TestMain(m *testing.M) {
	if os.Getenv("CONSIZE_TEST_DATABASE_URL") != "" || os.Getenv("CI") == "" {
		os.Exit(m.Run())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "consize-postgres-test-" + strconv.Itoa(os.Getpid())
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "-d", "--name", name, "-e", "POSTGRES_DB=consize_test", "-e", "POSTGRES_USER=consize", "-e", "POSTGRES_PASSWORD=consize-test-only", "-p", "127.0.0.1::5432", postgresTestImage)
	if output, err := command.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "start postgres test container: %v: %s\n", err, output)
		os.Exit(1)
	}
	portOutput, err := exec.CommandContext(ctx, "docker", "port", name, "5432/tcp").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve postgres test port:", err)
		os.Exit(1)
	}
	_, port, ok := strings.Cut(strings.TrimSpace(string(portOutput)), ":")
	if !ok {
		fmt.Fprintln(os.Stderr, "invalid postgres test port:", string(portOutput))
		os.Exit(1)
	}
	databaseURL := "postgres://consize:consize-test-only@127.0.0.1:" + port + "/consize_test?sslmode=disable"
	for {
		conn, connectErr := pgx.Connect(ctx, databaseURL)
		if connectErr == nil {
			_ = conn.Close(ctx)
			break
		}
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "postgres test container did not become ready:", connectErr)
			os.Exit(1)
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = os.Setenv("CONSIZE_TEST_DATABASE_URL", databaseURL)
	code := m.Run()
	if output, err := exec.Command("docker", "stop", name).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "stop postgres test container: %v: %s\n", err, output)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func postgresTestURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("CONSIZE_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("CONSIZE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("consize_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), base)
		if err == nil {
			_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			_ = conn.Close(context.Background())
		}
	})
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestPostgresFreshInstallConstraintsAndRestartRecovery(t *testing.T) {
	ctx := context.Background()
	databaseURL := postgresTestURL(t)
	before, err := InspectPostgres(ctx, databaseURL)
	if err != nil || before.CurrentVersion != 0 || before.MigrationStatus != MigrationPending {
		t.Fatalf("fresh diagnostics = %+v, err = %v", before, err)
	}
	st, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := st.StorageDiagnostics(ctx)
	if diagnostics.CurrentVersion != 2 || diagnostics.MigrationStatus != MigrationApplied {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	res, err := st.UpsertResource(ctx, completeResource("postgres-resource"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.CreateRecommendation(ctx, Recommendation{ResourceID: res.ID, PluginID: "plugin", ActionType: "resize", AlgorithmID: "algorithm", EvidenceRefs: []string{"evidence:1"}})
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.CreateAction(ctx, Action{RecommendationID: rec.ID, ResourceID: res.ID, RemediationPath: "direct_apply", PluginID: "plugin", ActionType: "resize", Mode: "approved", IdempotencyKey: "postgres-workflow", RequestedBy: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: rec.ID, ActionID: action.ID, RecommendationID: rec.ID, Resource: res, Plan: plugin.ActionPlan{PluginID: "plugin", ResourceID: res.ID, ActionType: "resize", OriginalState: map[string]any{"size": 10}, AppliedState: map[string]any{"size": 8}}, Actor: "operator", Deadline: time.Now().Add(time.Hour)}
	if _, err := st.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	st.Close()

	reopened, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	jobs, err := reopened.ListJobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].State != "prepared" {
		t.Fatalf("recovered jobs = %+v, err = %v", jobs, err)
	}
	duplicate := action
	duplicate.ID = 0
	got, err := reopened.CreateAction(ctx, duplicate)
	if err != nil || got.ID != action.ID {
		t.Fatalf("idempotent action = %+v, err = %v", got, err)
	}
	if _, err := reopened.pool.Exec(ctx, `INSERT INTO actions(recommendation_id,resource_id,idempotency_key,status,payload,created_at,updated_at) VALUES(999999,$1,'bad-fk','requested','{}',now(),now())`, res.ID); err == nil {
		t.Fatal("foreign key constraint accepted a missing recommendation")
	}
	var indexes int
	if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname IN ('actions_idempotency_key_key','jobs_one_active_per_resource_idx','recommendations_resource_status_idx')`).Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("required index count = %d, err = %v", indexes, err)
	}
}

func TestPostgresUpgradesPriorSchemaWithoutDataLoss(t *testing.T) {
	ctx := context.Background()
	databaseURL := postgresTestURL(t)
	migrations, err := loadPostgresMigrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations(version integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, migrations[0].SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations VALUES(1,$1,$2,now())`, migrations[0].Name, migrations[0].Checksum); err != nil {
		t.Fatal(err)
	}
	res, err := completeResource("upgrade-resource").Normalize(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(res)
	if _, err := conn.Exec(ctx, `INSERT INTO resources(id,provider,account_id,region,provider_resource_id,payload,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, res.ID, res.Provider, res.Account, res.Region, res.ProviderResourceID, payload, res.CreatedAt); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	st, err := OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetResource(ctx, res.ID); err != nil {
		t.Fatalf("resource lost during upgrade: %v", err)
	}
	if st.diagnostics.MigrationStatus != MigrationApplied || len(st.diagnostics.Migrations) != 1 || st.diagnostics.Migrations[0].ToVersion != 2 {
		t.Fatalf("migration diagnostics = %+v", st.diagnostics)
	}
}

func TestPostgresRejectsChangedAndPartialMigrationLedger(t *testing.T) {
	ctx := context.Background()
	databaseURL := postgresTestURL(t)
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations(version integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL); INSERT INTO schema_migrations VALUES(1,'001_initial.sql','tampered',now())`, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	if st, err := OpenPostgres(ctx, databaseURL); err == nil || !strings.Contains(err.Error(), "changed after application") {
		if st != nil {
			st.Close()
		}
		t.Fatalf("changed migration error = %v", err)
	}
}

func TestPostgresFailedMigrationRollsBackSchemaAndLedger(t *testing.T) {
	ctx := context.Background()
	databaseURL := postgresTestURL(t)
	migrations, err := loadPostgresMigrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations(version integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, migrations[0].SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations VALUES(1,$1,$2,now())`, migrations[0].Name, migrations[0].Checksum); err != nil {
		t.Fatal(err)
	}
	// Relation names share a namespace in PostgreSQL. This forces migration 002
	// to fail after its first two CREATE INDEX statements have run.
	if _, err := conn.Exec(ctx, `CREATE TABLE actions_recommendation_idx(marker integer)`); err != nil {
		t.Fatal(err)
	}
	if st, err := OpenPostgres(ctx, databaseURL); err == nil {
		st.Close()
		t.Fatal("partially applicable migration was accepted")
	}
	var applied int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("migration ledger count = %d, err = %v", applied, err)
	}
	var partialIndex bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.recommendations_resource_status_idx') IS NOT NULL`).Scan(&partialIndex); err != nil {
		t.Fatal(err)
	}
	if partialIndex {
		t.Fatal("failed migration left an earlier index behind")
	}
}
