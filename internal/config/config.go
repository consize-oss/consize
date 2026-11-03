package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/consize-oss/consize/internal/bootstrap"
	"github.com/consize-oss/consize/internal/store"
	"github.com/consize-oss/consize/pkg/resource"
)

func Load(path string, out any) error {
	return load(path, out, false)
}

func load(path string, out any, strict bool) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if strict {
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return fmt.Errorf("decode %s: expected exactly one configuration object", path)
		}
	}
	return nil
}

func LoadBootstrap(path string) (bootstrap.Config, error) {
	var cfg *bootstrap.Config
	if err := load(path, &cfg, true); err != nil {
		return bootstrap.Config{}, err
	}
	if cfg == nil {
		return bootstrap.Config{}, fmt.Errorf("configuration must be an object")
	}
	return *cfg, nil
}

func LoadResource(path string) (resource.Resource, error) {
	var res resource.Resource
	if err := Load(path, &res); err != nil {
		return resource.Resource{}, err
	}
	return res, nil
}

func LoadRecommendation(path string) (store.Recommendation, error) {
	var rec store.Recommendation
	if err := Load(path, &rec); err != nil {
		return store.Recommendation{}, err
	}
	return rec, nil
}
