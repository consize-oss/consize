package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/consize-oss/consize/pkg/plugin"
	"github.com/consize-oss/consize/pkg/plugin/marketplace"
	"github.com/consize-oss/consize/pkg/plugins/kubernetes"
	"io"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-manifest" {
		if err := json.NewEncoder(os.Stdout).Encode(kubernetes.NewWithPatcher(nil).Manifest()); err != nil {
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
	var cfg kubernetes.Config
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("invalid Kubernetes plugin configuration")
		}
	}
	return kubernetes.New(cfg)
}
