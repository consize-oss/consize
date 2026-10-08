package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/consize-oss/consize/internal/api"
	"github.com/consize-oss/consize/internal/audit"
	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/config"
	"github.com/consize-oss/consize/internal/cost"
	"github.com/consize-oss/consize/internal/discovery"
	"github.com/consize-oss/consize/internal/orchestrator"
	"github.com/consize-oss/consize/internal/policy"
	"github.com/consize-oss/consize/internal/recommender"
	"github.com/consize-oss/consize/internal/safety"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/resource"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "storage":
		return runStorage(ctx, args[1:])
	case "discover":
		fs := flag.NewFlagSet("discover", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, plugins, _, _, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		defer st.Close()
		results, err := discovery.New(st, plugins).Run(ctx)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"providers": results})
	case "worker":
		fs := flag.NewFlagSet("worker", flag.ExitOnError)
		path := fs.String("config", "", "Plugin and durable state configuration")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, plugins, policies, cfg, err := foundation(ctx, *path)
		if err != nil {
			return err
		}
		defer st.Close()
		if !st.Durable() {
			return fmt.Errorf("worker requires database_url or state_path")
		}
		return safety.New(st, plugins, policies, cfg.Verification, cfg.Recommender).Run(ctx)
	case "plugins":
		fs := flag.NewFlagSet("plugins", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, plugins, _, _, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		return printJSON(plugins.Manifests())
	case "health":
		fs := flag.NewFlagSet("health", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, plugins, _, _, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		out := map[string]plugin.Health{}
		for _, manifest := range plugins.Manifests() {
			health, err := plugins.Health(ctx, manifest.ID)
			if err != nil {
				return err
			}
			out[manifest.ID] = health
		}
		return printJSON(out)
	case "metrics":
		fs := flag.NewFlagSet("metrics", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		resourcePath := fs.String("resource", "", "Path to resource JSON")
		pluginID := fs.String("plugin", "prometheus-metrics", "Metrics plugin ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		res, err := config.LoadResource(*resourcePath)
		if err != nil {
			return err
		}
		_, plugins, _, _, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		p, err := plugins.MetricsPlugin(*pluginID, res.Type)
		if err != nil {
			return err
		}
		snapshot, err := p.ReadMetrics(ctx, res)
		if err != nil {
			return err
		}
		return printJSON(snapshot)
	case "recommend":
		fs := flag.NewFlagSet("recommend", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		resourcePath := fs.String("resource", "", "Path to resource JSON")
		pluginID := fs.String("plugin", "", "Metrics plugin ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		res, err := config.LoadResource(*resourcePath)
		if err != nil {
			return err
		}
		_, plugins, _, cfg, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		metricsPluginID := *pluginID
		if metricsPluginID == "" {
			metricsPluginID = cfg.Recommender.MetricsPluginID
		}
		if metricsPluginID == "" {
			metricsPluginID = cfg.Verification.MetricsPluginID
		}
		if metricsPluginID == "" {
			metricsPluginID = "prometheus-metrics"
		}
		p, err := plugins.MetricsPlugin(metricsPluginID, res.Type)
		if err != nil {
			return err
		}
		snapshot, err := p.ReadMetrics(ctx, res)
		if err != nil {
			return err
		}
		recs, err := recommender.New(cfg.Recommender).Recommend(ctx, recommender.Input{
			Resource: res,
			Evidence: []plugin.MetricsSnapshot{
				snapshot,
			},
		})
		if err != nil {
			return err
		}
		if cfg.Pricing.Enabled {
			for i := range recs {
				recs[i], err = cost.Enrich(ctx, plugins, cfg.Pricing.EffectivePluginID(), res, recs[i])
				if err != nil {
					return err
				}
			}
		}
		return printJSON(map[string]any{"recommendations": recs, "evidence": snapshot})
	case "plan", "execute":
		fs := flag.NewFlagSet(args[0], flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		resourcePath := fs.String("resource", "", "Path to resource JSON")
		recommendationPath := fs.String("recommendation", "", "Path to recommendation JSON")
		mode := fs.String("mode", "dry_run", "Action mode: dry_run, approved, or auto")
		actor := fs.String("actor", "", "Actor requesting the action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "execute" && *mode == "dry_run" {
			*mode = "approved"
		}
		st, plugins, policies, cfg, err := foundation(ctx, *configPath)
		if err != nil {
			return err
		}
		res, err := config.LoadResource(*resourcePath)
		if err != nil {
			return err
		}
		rec, err := config.LoadRecommendation(*recommendationPath)
		if err != nil {
			return err
		}
		res, err = st.UpsertResource(ctx, res)
		if err != nil {
			return err
		}
		rec.ResourceID = res.ID
		rec, err = st.CreateRecommendation(ctx, rec)
		if err != nil {
			return err
		}
		if args[0] == "plan" {
			*mode = "dry_run"
		}
		if *mode != "dry_run" {
			if *mode != "approved" {
				return fmt.Errorf("execution requires explicit approved mode")
			}
			return runDurableJob(ctx, st, plugins, policies, cfg, rec.ID, *actor)
		}
		out, err := orchestrator.New(st, plugins, policies).ExecuteRecommendation(ctx, rec.ID, *mode, *actor)
		if err != nil {
			return err
		}
		if err := writeAudit(cfg, "action", out); err != nil {
			return err
		}
		return printJSON(out)
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		configPath := fs.String("config", "", "Path to Consize plugin config JSON")
		resourcePath := fs.String("resource", "", "Path to resource JSON")
		recommendationPath := fs.String("recommendation", "", "Optional path to recommendation JSON")
		mode := fs.String("mode", "approved", "Action mode: dry_run, approved, or auto")
		actor := fs.String("actor", "", "Actor requesting the action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runEndToEnd(ctx, *configPath, *resourcePath, *recommendationPath, *mode, *actor)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		configPath := fs.String("config", "", "Optional path to Consize plugin config JSON")
		addr := fs.String("addr", "127.0.0.1:8080", "HTTP listen address")
		resourcePath := fs.String("resource", "", "Optional resource JSON to seed")
		recommendationPath := fs.String("recommendation", "", "Optional recommendation JSON to seed")
		demo := fs.Bool("demo", false, "Explicitly seed sample data for design review only")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return serveHTTP(ctx, *configPath, *addr, *resourcePath, *recommendationPath, *demo)
	default:
		return usage()
	}
}

func foundation(ctx context.Context, path string) (store.RuntimeStore, *plugin.Manager, *policy.Engine, bootstrap.Config, error) {
	cfg, err := config.LoadBootstrap(path)
	if err != nil {
		return nil, nil, nil, bootstrap.Config{}, err
	}
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = os.Getenv("CONSIZE_DATABASE_URL")
	}
	var st store.RuntimeStore = store.NewMemory()
	if cfg.DatabaseURL != "" {
		st, err = store.OpenPostgres(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, nil, bootstrap.Config{}, err
		}
	} else if cfg.StatePath != "" {
		st, err = store.OpenDurable(cfg.StatePath)
		if err != nil {
			return nil, nil, nil, bootstrap.Config{}, err
		}
	}
	plugins := plugin.NewManager()
	if err := bootstrap.RegisterConfiguredPlugins(ctx, plugins, cfg); err != nil {
		st.Close()
		return nil, nil, nil, bootstrap.Config{}, err
	}
	return st, plugins, policy.NewEngine(), cfg, nil
}

func usage() error {
	return fmt.Errorf("usage: consize <discover|plugins|health|metrics|recommend|plan|execute|run|serve|storage> [flags]")
}

const defaultStorageOperationTimeout = 30 * time.Second

func runStorage(parent context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: consize storage <status|migrate|backup|restore|reset> -state path [flags]")
	}
	fs := flag.NewFlagSet("storage "+args[0], flag.ContinueOnError)
	statePath := fs.String("state", "", "Path to the durable Consize state file")
	databaseURL := fs.String("database-url", os.Getenv("CONSIZE_DATABASE_URL"), "PostgreSQL connection URL (prefer CONSIZE_DATABASE_URL)")
	backupPath := fs.String("backup", "", "Path to the backup file")
	confirmation := fs.String("confirm", "", "Required reset confirmation phrase")
	timeout := fs.Duration("timeout", defaultStorageOperationTimeout, "Maximum time for a PostgreSQL storage operation")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if (*statePath == "") == (*databaseURL == "") {
		return fmt.Errorf("exactly one of -state or -database-url is required")
	}
	if *databaseURL != "" {
		if *timeout <= 0 {
			return fmt.Errorf("-timeout must be greater than zero")
		}
		if args[0] != "status" && args[0] != "migrate" {
			return fmt.Errorf("postgres %s uses the documented pg_dump/pg_restore procedure; only status and migrate are supported here", args[0])
		}
		ctx, cancel := context.WithTimeout(parent, *timeout)
		defer cancel()
		if args[0] == "status" {
			diagnostics, err := store.InspectPostgres(ctx, *databaseURL)
			if printErr := printJSON(diagnostics); printErr != nil {
				return printErr
			}
			return err
		}
		st, err := store.OpenPostgres(ctx, *databaseURL)
		if err != nil {
			return err
		}
		defer st.Close()
		return printJSON(st.StorageDiagnostics(ctx))
	}
	switch args[0] {
	case "status":
		diagnostics, err := store.InspectDurable(*statePath)
		if err != nil {
			_ = printJSON(diagnostics)
			return err
		}
		return printJSON(diagnostics)
	case "migrate":
		st, err := store.OpenDurable(*statePath)
		if err != nil {
			return err
		}
		defer st.Close()
		return printJSON(st.StorageDiagnostics(context.Background()))
	case "backup":
		if *backupPath == "" {
			return fmt.Errorf("-backup is required")
		}
		diagnostics, err := store.BackupDurable(*statePath, *backupPath)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"backup": *backupPath, "checksum": *backupPath + ".sha256", "storage": diagnostics})
	case "restore":
		if *backupPath == "" {
			return fmt.Errorf("-backup is required")
		}
		diagnostics, err := store.RestoreDurable(*statePath, *backupPath)
		if err != nil {
			return err
		}
		return printJSON(diagnostics)
	case "reset":
		archive, err := store.ResetDurable(*statePath, *confirmation)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"reset": true, "previous_state_archive": archive})
	default:
		return fmt.Errorf("unknown storage command %q", args[0])
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runEndToEnd(ctx context.Context, configPath, resourcePath, recommendationPath, mode, actor string) error {
	if mode != "dry_run" && mode != "approved" {
		return fmt.Errorf("end-to-end execution requires dry_run or approved mode")
	}
	st, plugins, policies, cfg, err := foundation(ctx, configPath)
	if err != nil {
		return err
	}
	res, err := config.LoadResource(resourcePath)
	if err != nil {
		return err
	}
	res, err = st.UpsertResource(ctx, res)
	if err != nil {
		return err
	}
	metricsPluginID := cfg.Verification.MetricsPluginID
	if metricsPluginID == "" {
		metricsPluginID = cfg.Recommender.MetricsPluginID
	}
	if metricsPluginID == "" {
		metricsPluginID = "prometheus-metrics"
	}
	metricsPlugin, err := plugins.MetricsPlugin(metricsPluginID, res.Type)
	if err != nil {
		return err
	}
	before, err := metricsPlugin.ReadMetrics(ctx, res)
	if err != nil {
		return err
	}
	var rec store.Recommendation
	if recommendationPath != "" {
		rec, err = config.LoadRecommendation(recommendationPath)
		if err != nil {
			return err
		}
	} else {
		recs, err := recommender.New(cfg.Recommender).Recommend(ctx, recommender.Input{
			Resource: res,
			Evidence: []plugin.MetricsSnapshot{
				before,
			},
		})
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return fmt.Errorf("recommender returned no recommendations")
		}
		rec = recs[0]
	}
	rec.ResourceID = res.ID
	if cfg.Pricing.Enabled {
		rec, err = cost.Enrich(ctx, plugins, cfg.Pricing.EffectivePluginID(), res, rec)
		if err != nil {
			return err
		}
	}
	rec, err = st.CreateRecommendation(ctx, rec)
	if err != nil {
		return err
	}
	if mode != "dry_run" {
		return runDurableJob(ctx, st, plugins, policies, cfg, rec.ID, actor)
	}
	action, err := orchestrator.New(st, plugins, policies).ExecuteRecommendation(ctx, rec.ID, mode, actor)
	if err != nil {
		return err
	}
	if err := writeAudit(cfg, "action", action); err != nil {
		return err
	}
	return printJSON(map[string]any{
		"recommendation": rec,
		"before":         before,
		"action":         action,
		"verification":   nil,
	})
}

func writeAudit(cfg bootstrap.Config, recordType string, payload any) error {
	if cfg.Audit.Path == "" {
		return nil
	}
	sink, err := audit.NewJSONLSink(cfg.Audit.Path)
	if err != nil {
		return err
	}
	return sink.Write(recordType, payload)
}

func serveHTTP(ctx context.Context, configPath, addr, resourcePath, recommendationPath string, demo bool) error {
	st, plugins, policies, cfg, err := optionalFoundation(ctx, configPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := validateListenAddress(addr, cfg.Auth.Enabled); err != nil {
		return err
	}
	var seededResource resource.Resource
	if resourcePath != "" {
		seededResource, err = config.LoadResource(resourcePath)
		if err != nil {
			return err
		}
		if existing, loadErr := st.GetResource(ctx, seededResource.ID); loadErr == nil {
			seededResource = existing
		} else if loadErr != store.ErrNotFound {
			return loadErr
		}
		seededResource, err = st.UpsertResource(ctx, seededResource)
		if err != nil {
			return err
		}
	}
	if recommendationPath != "" {
		rec, err := config.LoadRecommendation(recommendationPath)
		if err != nil {
			return err
		}
		if rec.ResourceID == "" {
			rec.ResourceID = seededResource.ID
		}
		if rec.ResourceID == "" {
			return fmt.Errorf("recommendation resource_id is required when no resource seed is provided")
		}
		if _, err := st.CreateRecommendation(ctx, rec); err != nil {
			return err
		}
	}
	server, err := api.NewServer(st, plugins, policies, cfg)
	if err != nil {
		return err
	}
	if demo {
		if err := server.SeedDemo(ctx); err != nil {
			return err
		}
	}
	fmt.Printf("Consize API listening on http://%s\n", addr)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	workerDone := make(chan error, 1)
	go func() {
		err := server.RunWorker(workerCtx)
		workerDone <- err
		if err != nil {
			httpServer.Close()
		}
	}()
	go func() {
		<-workerCtx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		httpServer.Shutdown(shutdownCtx)
	}()
	err = httpServer.ListenAndServe()
	cancel()
	workerErr := <-workerDone
	if workerErr != nil {
		return workerErr
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func validateListenAddress(addr string, authenticationEnabled bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if authenticationEnabled || host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("authentication must be enabled before listening on non-loopback address %q", addr)
	}
	return nil
}

func runDurableJob(ctx context.Context, st store.RuntimeStore, plugins *plugin.Manager, policies *policy.Engine, cfg bootstrap.Config, id int64, actor string) error {
	controller := safety.New(st, plugins, policies, cfg.Verification, cfg.Recommender)
	if _, err := controller.Submit(ctx, id, actor); err != nil {
		return err
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := controller.Tick(ctx); err != nil {
			return err
		}
		jobs, err := st.ListJobs(ctx)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.ID == id && store.Terminal(job.State) {
				if err := printJSON(job); err != nil {
					return err
				}
				if job.State != "verified" {
					return fmt.Errorf("action finished as %s", job.State)
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func optionalFoundation(ctx context.Context, path string) (store.RuntimeStore, *plugin.Manager, *policy.Engine, bootstrap.Config, error) {
	if path != "" {
		return foundation(ctx, path)
	}
	return store.NewMemory(), plugin.NewManager(), policy.NewEngine(), bootstrap.Config{}, nil
}
