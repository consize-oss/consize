package cibaseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflow struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency map[string]any    `yaml:"concurrency"`
	Jobs        map[string]struct {
		Name    string `yaml:"name"`
		Timeout int    `yaml:"timeout-minutes"`
		If      string `yaml:"if"`
		Steps   []struct {
			Uses string         `yaml:"uses"`
			Run  string         `yaml:"run"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../..", path))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBaselineCannotSilentlySkipOrMutate(t *testing.T) {
	var w workflow
	if err := yaml.Unmarshal(read(t, ".github/workflows/ci.yml"), &w); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"pull_request", "push", "merge_group"} {
		value, ok := w.On[event]
		if !ok {
			t.Fatalf("missing required event %s", event)
		}
		if filters, ok := value.(map[string]any); ok {
			for _, key := range []string{"paths", "paths-ignore"} {
				if _, ok := filters[key]; ok {
					t.Fatalf("%s hides required checks with %s", event, key)
				}
			}
		}
	}
	if w.Permissions["contents"] != "read" || len(w.Permissions) != 1 {
		t.Fatal("baseline token must be contents:read only")
	}
	if w.Concurrency["cancel-in-progress"] != true {
		t.Fatal("superseded runs must be cancelled")
	}
	pinned := regexp.MustCompile(`@[a-f0-9]{40}$`)
	for id, job := range w.Jobs {
		if job.Timeout <= 0 || job.Timeout > 30 {
			t.Fatalf("%s lacks a bounded timeout", id)
		}
		for _, step := range job.Steps {
			if step.Uses != "" && !pinned.MatchString(step.Uses) {
				t.Fatalf("%s has mutable action %s", id, step.Uses)
			}
		}
	}
	for id, name := range map[string]string{"backend": "Backend quality", "frontend": "Frontend quality", "browser": "Browser smoke"} {
		job, ok := w.Jobs[id]
		if !ok || job.Name != name || job.If != "" {
			t.Fatalf("required job %s missing, renamed or conditional", id)
		}
		commands := ""
		for _, step := range job.Steps {
			commands += step.Run + "\n"
		}
		for _, forbidden := range []string{"--fix", "npm audit fix", "continue-on-error", "|| true"} {
			if strings.Contains(commands, forbidden) {
				t.Fatalf("%s masks or repairs a failure", id)
			}
		}
		required := map[string][]string{
			"backend":  {"scripts/ci/backend.sh", "git diff --exit-code"},
			"frontend": {"npm ci", "npm run lint", "npm run typecheck", "npm test", "npm run build", "check-generated.sh", "git diff --exit-code"},
			"browser":  {"go build -mod=readonly", "cli.js install", "npm run build", "npm run test:e2e", "git diff --exit-code"},
		}
		for _, command := range required[id] {
			if !strings.Contains(commands, command) {
				t.Fatalf("%s no longer runs %s", id, command)
			}
		}
	}
}

func TestToolVersionsAndLockfileAgree(t *testing.T) {
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
			NPM  string `json:"npm"`
		} `json:"engines"`
		PackageManager string            `json:"packageManager"`
		Dev            map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(read(t, "tools/ci/package.json"), &pkg); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(read(t, ".node-version"))) != pkg.Engines.Node {
		t.Fatal("Node declarations disagree")
	}
	if pkg.PackageManager != "npm@"+pkg.Engines.NPM {
		t.Fatal("npm declarations disagree")
	}
	exact := regexp.MustCompile(`^(\d+\.\d+\.\d+|https://registry\.npmjs\.org/@playwright/test/-/test-\d+\.\d+\.\d+\.tgz)$`)
	for _, name := range []string{"eslint", "eslint-config-next", "@playwright/test"} {
		if !exact.MatchString(pkg.Dev[name]) {
			t.Fatalf("%s must be pinned", name)
		}
	}
	var lock struct {
		Packages map[string]struct {
			Dev map[string]string `json:"devDependencies"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(read(t, "tools/ci/package-lock.json"), &lock); err != nil {
		t.Fatal(err)
	}
	for name, version := range pkg.Dev {
		if lock.Packages[""].Dev[name] != version {
			t.Fatalf("lockfile drift for %s", name)
		}
	}
}
