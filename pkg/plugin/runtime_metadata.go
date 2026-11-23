package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Static declarations are callbacks too. Limit both waiting and the number of
// stranded callbacks if an in-process provider violates this contract.
var metadataSlots = make(chan struct{}, 16)

type definition struct {
	id       string
	manifest Manifest
	err      error
}

func describe(p Plugin) (string, Manifest, error) {
	select {
	case metadataSlots <- struct{}{}:
	default:
		return "", Manifest{}, &RuntimeError{Code: FailureBusy}
	}
	done := make(chan definition, 1)
	go func() {
		var out definition
		defer func() {
			if recover() != nil {
				out = definition{err: &RuntimeError{Code: FailureInternal}}
			}
			<-metadataSlots
			done <- out
		}()
		out.id = p.ID()
		out.manifest = copyManifest(p.Manifest())
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case out := <-done:
		return out.id, out.manifest, out.err
	case <-timer.C:
		return "", Manifest{}, &RuntimeError{Code: FailureTimeout}
	}
}
func captureRuntimeMetadata(ctx context.Context, e *runtimeEntry) error {
	if e.manifest.Runtime != nil && e.manifest.Runtime.Distribution == "external" {
		if _, ok := e.raw.(ArtifactIdentity); !ok {
			return &RuntimeError{Code: FailureIncompatible}
		}
	}
	if _, external := e.raw.(ArtifactIdentity); !external {
		digest, err := hostIntegrity()
		if err != nil {
			return &RuntimeError{Code: FailureInternal}
		}
		e.mu.Lock()
		e.status.Identity.Integrity = digest
		e.mu.Unlock()
	}
	if artifact, ok := e.raw.(ArtifactIdentity); ok {
		digest, err := run(ctx, e, modeLifecycle, func(context.Context) (string, error) {
			return strings.TrimPrefix(artifact.ArtifactDigest(), "sha256:"), nil
		})
		if err != nil {
			return err
		}
		if !integrityIdentity.MatchString("sha256:" + digest) {
			return &RuntimeError{Code: FailureInvalidResponse}
		}
		if e.manifest.Runtime != nil && e.manifest.Runtime.Integrity != "" && e.manifest.Runtime.Integrity != "sha256:"+digest {
			return &RuntimeError{Code: FailureIncompatible}
		}
		e.mu.Lock()
		e.status.Identity.Integrity = "sha256:" + digest
		e.mu.Unlock()
	}
	if scoped, ok := e.raw.(ScopedDiscoveryPlugin); ok {
		scope, err := run(ctx, e, modeLifecycle, func(context.Context) (map[string]string, error) { return freeze(scoped.DiscoveryScope()) })
		if err != nil {
			return err
		}
		e.scope = scope
	}
	return nil
}

var hostIdentity struct {
	sync.Once
	digest string
	err    error
}

func hostIntegrity() (string, error) {
	hostIdentity.Do(func() {
		path, err := os.Executable()
		if err != nil {
			hostIdentity.err = err
			return
		}
		file, err := os.Open(path)
		if err != nil {
			hostIdentity.err = err
			return
		}
		defer file.Close()
		hash := sha256.New()
		if _, err = io.Copy(hash, file); err != nil {
			hostIdentity.err = err
			return
		}
		hostIdentity.digest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	})
	return hostIdentity.digest, hostIdentity.err
}
