package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugin/marketplace"
	"github.com/consize-oss/consize/pkg/plugins/prometheus"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-manifest" {
		if err := json.NewEncoder(os.Stdout).Encode(prometheus.NewWithClient(nil, prometheus.Config{}).Manifest()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	err := marketplace.ServeOnce(context.Background(), os.Stdin, os.Stdout, configuredPlugin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func configuredPlugin(raw json.RawMessage) (plugin.Plugin, error) {
	var cfg struct {
		Transport      prometheus.TransportConfig `json:"transport,omitempty"`
		BaseURL        string                     `json:"base_url"`
		Window         string                     `json:"window"`
		Step           string                     `json:"step"`
		Queries        map[string]string          `json:"queries"`
		Units          map[string]string          `json:"units,omitempty"`
		Mapping        prometheus.MappingConfig   `json:"mapping,omitempty"`
		Percentile     int                        `json:"percentile,omitempty"`
		MinimumSamples int                        `json:"minimum_samples,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("invalid Prometheus plugin configuration")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("invalid Prometheus plugin configuration")
	}
	var window, step time.Duration
	var err error
	if cfg.Window != "" {
		window, err = time.ParseDuration(cfg.Window)
		if err != nil {
			return nil, fmt.Errorf("invalid Prometheus collection duration")
		}
	}
	if cfg.Step != "" {
		step, err = time.ParseDuration(cfg.Step)
		if err != nil {
			return nil, fmt.Errorf("invalid Prometheus collection duration")
		}
	}
	return prometheus.New(prometheus.Config{BaseURL: cfg.BaseURL, Transport: cfg.Transport, Window: window, Step: step, Queries: cfg.Queries, Units: cfg.Units, Mapping: cfg.Mapping, Percentile: cfg.Percentile, MinimumSamples: cfg.MinimumSamples})
}
