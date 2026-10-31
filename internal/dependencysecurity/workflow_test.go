package dependencysecurity

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependencyWorkflowCannotSkipOrMaskTheGate(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/dependency-security.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On          map[string]any    `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Name     string `yaml:"name"`
			Timeout  int    `yaml:"timeout-minutes"`
			If       string `yaml:"if"`
			Continue bool   `yaml:"continue-on-error"`
			Steps    []struct {
				Run      string `yaml:"run"`
				Continue bool   `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"pull_request", "push", "merge_group", "schedule", "workflow_dispatch"} {
		if _, ok := workflow.On[event]; !ok {
			t.Fatalf("missing trigger %s", event)
		}
	}
	if workflow.Permissions["contents"] != "read" || len(workflow.Permissions) != 1 {
		t.Fatal("gate needs only contents:read")
	}
	job, ok := workflow.Jobs["dependency-security"]
	if !ok || job.Name != "Dependency security" || job.If != "" || job.Continue || job.Timeout <= 0 || job.Timeout > 30 {
		t.Fatal("dependency check is missing, conditional, unbounded or allowed to fail")
	}
	for _, step := range job.Steps {
		if step.Continue || strings.Contains(step.Run, "|| true") || strings.Contains(step.Run, "audit fix") {
			t.Fatal("dependency failures must not be hidden or auto-fixed")
		}
	}
	for _, bad := range []string{"pull_request_target:", "paths:", "paths-ignore:", "@latest"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("unsafe workflow pattern %s", bad)
		}
	}
	if !strings.Contains(string(b), "--revision") || !strings.Contains(string(b), "--govulncheck") {
		t.Fatal("missing scan identity or pinned tool")
	}
}
