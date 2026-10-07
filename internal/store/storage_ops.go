package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func InspectDurable(path string) (StorageDiagnostics, error) {
	diagnostics := StorageDiagnostics{Backend: "json-file", Durable: true, TargetVersion: currentStateVersion, Indexes: RequiredIndexes()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		diagnostics.Healthy = true
		diagnostics.CurrentVersion = 0
		diagnostics.MigrationStatus = MigrationPending
		return diagnostics, nil
	}
	if err != nil {
		return diagnostics, err
	}
	state, err := decodeDiskState(data)
	if err != nil {
		diagnostics.MigrationStatus = MigrationInvalid
		diagnostics.Error = err.Error()
		return diagnostics, err
	}
	diagnostics.LoadedVersion = state.Version
	diagnostics.CurrentVersion = state.Version
	diagnostics.MigrationStatus = MigrationCurrent
	if state.Version != currentStateVersion {
		diagnostics.MigrationStatus = MigrationPending
	}
	if _, err := migrateState(&state); err != nil {
		diagnostics.MigrationStatus = MigrationInvalid
		diagnostics.Error = err.Error()
		return diagnostics, err
	}
	if err := validateState(state); err != nil {
		diagnostics.MigrationStatus = MigrationInvalid
		diagnostics.Error = err.Error()
		return diagnostics, err
	}
	tmp := NewMemory()
	tmp.resources, tmp.recs, tmp.actionRecords, tmp.actions, tmp.jobs = state.Resources, state.Recommendations, state.ActionRecords, state.Actions, state.Jobs
	if err := tmp.rebuildIndexesLocked(); err != nil {
		diagnostics.MigrationStatus = MigrationInvalid
		diagnostics.Error = err.Error()
		return diagnostics, err
	}
	diagnostics.Healthy = true
	diagnostics.Migrations = append([]MigrationRecord(nil), state.Migrations...)
	diagnostics.EntityCounts = map[string]int{"resources": len(state.Resources), "recommendations": len(state.Recommendations), "actions": len(state.ActionRecords), "audit_events": len(state.Actions), "jobs": len(state.Jobs)}
	return diagnostics, nil
}

func BackupDurable(path, destination string) (StorageDiagnostics, error) {
	if path == "" || destination == "" || filepath.Clean(path) == filepath.Clean(destination) {
		return StorageDiagnostics{}, errors.New("distinct state and backup paths are required")
	}
	st, err := OpenDurable(path)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	defer st.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	if err := atomicWrite(destination, data, 0600); err != nil {
		return StorageDiagnostics{}, err
	}
	digest := sha256.Sum256(data)
	if err := atomicWrite(destination+".sha256", []byte(hex.EncodeToString(digest[:])+"  "+filepath.Base(destination)+"\n"), 0644); err != nil {
		return StorageDiagnostics{}, err
	}
	return st.StorageDiagnostics(context.Background()), nil
}

func RestoreDurable(path, backup string) (StorageDiagnostics, error) {
	if path == "" || backup == "" || filepath.Clean(path) == filepath.Clean(backup) {
		return StorageDiagnostics{}, errors.New("distinct state and backup paths are required")
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	checksum, err := os.ReadFile(backup + ".sha256")
	if err != nil {
		return StorageDiagnostics{}, fmt.Errorf("backup checksum is required: %w", err)
	}
	want := strings.Fields(string(checksum))
	digest := sha256.Sum256(data)
	if len(want) == 0 || want[0] != hex.EncodeToString(digest[:]) {
		return StorageDiagnostics{}, errors.New("backup checksum mismatch")
	}
	state, err := decodeDiskState(data)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	if _, err := migrateState(&state); err != nil {
		return StorageDiagnostics{}, err
	}
	if err := validateState(state); err != nil {
		return StorageDiagnostics{}, err
	}
	data, err = json.Marshal(state)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	lock, err := acquireStateLock(path)
	if err != nil {
		return StorageDiagnostics{}, err
	}
	defer lock.Close()
	if err := atomicWrite(path, data, 0600); err != nil {
		return StorageDiagnostics{}, err
	}
	return InspectDurable(path)
}

// ResetDurable moves the previous state aside instead of deleting it.
func ResetDurable(path, confirmation string) (string, error) {
	if confirmation != "ERASE LOCAL STATE" {
		return "", errors.New("reset requires confirmation: ERASE LOCAL STATE")
	}
	lock, err := acquireStateLock(path)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	archive := fmt.Sprintf("%s.reset-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, archive); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	return archive, nil
}

func decodeDiskState(data []byte) (diskState, error) {
	var state diskState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil {
		return diskState{}, fmt.Errorf("invalid state file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return diskState{}, errors.New("invalid state file: trailing data")
	}
	return state, nil
}

func acquireStateLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("state store already owned: %w", err)
	}
	return lock, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".consize-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
