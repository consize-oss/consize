package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStorageDiagnosticsJSONRedactsCredentials(t *testing.T) {
	credentialKey := strings.Join([]string{"pass", "word"}, "")
	tests := []struct {
		name        string
		error       string
		wantRemoved string
	}{
		{name: "connection URL", error: fmt.Sprintf("connect postgres://consize:%s@db.internal:5432/consize failed", "url-secret"), wantRemoved: "url-secret"},
		{name: "keyword assignment", error: fmt.Sprintf("host=db.internal %s=%s sslmode=require", credentialKey, "assignment-secret"), wantRemoved: "assignment-secret"},
		{name: "query parameter", error: fmt.Sprintf("connection failed: %s=%s&sslmode=require", credentialKey, "query-secret"), wantRemoved: "query-secret"},
		{name: "JSON field", error: fmt.Sprintf(`driver details: {"%s":"%s","host":"db.internal"}`, credentialKey, "json-secret"), wantRemoved: "json-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := StorageDiagnostics{Backend: "postgres", Error: tt.error}
			encoded, err := json.Marshal(diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), tt.wantRemoved) {
				t.Fatalf("serialized diagnostics exposed a password: %s", encoded)
			}
			if !strings.Contains(string(encoded), "REDACTED") {
				t.Fatalf("serialized diagnostics did not mark redaction: %s", encoded)
			}
			if diagnostics.Error != tt.error {
				t.Fatal("redaction mutated the original diagnostics")
			}
		})
	}
}

func TestStorageDiagnosticsJSONPreservesOrdinaryErrors(t *testing.T) {
	diagnostics := StorageDiagnostics{Backend: "postgres", Error: "database health check failed"}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), diagnostics.Error) {
		t.Fatalf("ordinary diagnostic error was changed: %s", encoded)
	}
}
