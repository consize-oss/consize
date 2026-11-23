package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"github.com/consize-oss/consize/pkg/plugins/pricing"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

func decodeInstance(raw json.RawMessage, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return errors.New("invalid package configuration; check its configuration reference")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("package configuration must contain exactly one object")
	}
	return nil
}

// Provider construction is restricted to this composition root. Workers only
// receive SDK-managed handles, including when several clusters are configured.
func registerInstances(ctx context.Context, m *plugin.Manager, cfg Config) error {

	factories := []plugin.PackageFactory{
		{Manifest: (&kubernetes.Plugin{}).Manifest(), Configure: func(_ context.Context, raw json.RawMessage) (plugin.Plugin, error) {
			var c KubernetesConfig
			if err := decodeInstance(raw, &c); err != nil {
				return nil, err
			}
			return kubernetes.New(kubernetes.Config{Kubeconfig: c.Kubeconfig, DiscoveryAuth: c.DiscoveryAuth, Context: c.Context, Discovery: c.Discovery, ClusterID: c.ClusterID, Location: c.Location})
		}},
		{Manifest: (&prometheus.Plugin{}).Manifest(), Configure: func(_ context.Context, raw json.RawMessage) (plugin.Plugin, error) {
			var c PrometheusConfig
			if err := decodeInstance(raw, &c); err != nil {
				return nil, err
			}
			window, err := parseOptionalDuration(c.Window)
			if err != nil {
				return nil, errors.New("invalid metrics window")
			}
			step, err := parseOptionalDuration(c.Step)
			if err != nil {
				return nil, errors.New("invalid metrics step")
			}
			return prometheus.New(prometheus.Config{BaseURL: c.BaseURL, Transport: c.Transport, Mapping: c.Mapping, Percentile: c.Percentile, MinimumSamples: c.MinimumSamples, Window: window, Step: step, Queries: c.Queries, Units: c.Units})
		}},
		{Manifest: (&pricing.Plugin{}).Manifest(), Configure: func(_ context.Context, raw json.RawMessage) (plugin.Plugin, error) {
			var c pricing.Config
			if err := decodeInstance(raw, &c); err != nil {
				return nil, err
			}
			return pricing.New(c)
		}},
	}
	for _, factory := range factories {
		if err := m.RegisterPackage(factory); err != nil {
			return err
		}
	}
	secrets := map[string]string{}
	for ref, env := range cfg.PluginSecretEnv {
		if value, ok := os.LookupEnv(env); ok {
			secrets[ref] = value
		}
	}
	instances := append([]plugin.InstanceConfig(nil), cfg.PluginInstances...)
	for _, legacy := range []struct {
		enabled                                bool
		packageID, provider, account, location string
		settings                               any
	}{{cfg.Kubernetes.Enabled, kubernetes.ID, "kubernetes", cfg.Kubernetes.ClusterID, cfg.Kubernetes.Location, cfg.Kubernetes}, {cfg.Prometheus.Enabled, prometheus.ID, "kubernetes", "", "", cfg.Prometheus}, {cfg.Pricing.Enabled, pricing.ID, "kubernetes", "", "", pricing.Config{Currency: cfg.Pricing.Currency, Source: cfg.Pricing.Source, MemoryGiBMonthly: cfg.Pricing.MemoryGiBMonthly, CPUCoreMonthly: cfg.Pricing.CPUCoreMonthly}}} {
		if legacy.enabled {
			raw, err := json.Marshal(legacy.settings)
			if err != nil {
				return errors.New("invalid legacy plugin configuration")
			}
			instances = append(instances, plugin.InstanceConfig{ID: legacy.packageID, PackageID: legacy.packageID, Required: true, Provider: legacy.provider, Account: legacy.account, Location: legacy.location, Configuration: raw})
		}
	}
	for index, instance := range instances {
		if index < len(cfg.PluginInstances) && (instance.Provider == "" || instance.Account == "" || instance.Location == "") {
			return errors.New("configured plugin instances require provider, account and location routing scope")
		}
		if err := m.Install(ctx, instance, secrets); err != nil {
			return err
		}
	}
	return nil
}
