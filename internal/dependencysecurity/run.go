package dependencysecurity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/consize-oss/consize/internal/securitypolicy"
)

type RunConfig struct {
	Root, Output, NPM, Govulncheck, Revision, Context string
	Timeout                                           time.Duration
}
type processResult struct {
	Output, Stderr []byte
	Exit           int
	TimedOut       bool
}
type executor func(context.Context, string, []string, string, []string) processResult

type receipt struct {
	Platform         string `json:"platform"`
	GoVersion        string `json:"go_version,omitempty"`
	DatabaseModified string `json:"database_modified,omitempty"`
	ID               string `json:"id"`
	Scanner          string `json:"scanner"`
	ToolVersion      string `json:"tool_version"`
	Revision         string `json:"revision"`
	InputSHA256      string `json:"input_sha256"`
	Scope            string `json:"scope"`
	Status           string `json:"status"`
	Attempts         int    `json:"attempts"`
	ExitCode         int    `json:"exit_code"`
	Gate             string `json:"gate"`
}
type scanSpec struct {
	id, scanner, path, dir, command, version, scope string
	args                                            []string
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxReportBytes {
		return 0, fmt.Errorf("scanner output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func execute(ctx context.Context, name string, args []string, dir string, env []string) processResult {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = env
	command.WaitDelay = 2 * time.Second
	var stdout, stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exit := 0
	if err != nil {
		exit = -1
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			exit = failure.ExitCode()
		}
	}
	return processResult{Output: stdout.Bytes(), Stderr: stderr.Bytes(), Exit: exit, TimedOut: ctx.Err() != nil}
}

func Run(ctx context.Context, c RunConfig) error {
	if c.Root == "" || c.Output == "" || c.Revision == "" {
		return fmt.Errorf("root, output and revision required")
	}
	if c.Timeout <= 0 {
		c.Timeout = 4 * time.Minute
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return err
	}
	c.Root = root
	output, err := filepath.Abs(c.Output)
	if err != nil {
		return err
	}
	c.Output = output
	if err := os.MkdirAll(c.Output, 0o700); err != nil {
		return err
	}
	policy, err := securitypolicy.LoadPolicy(filepath.Join(root, ".github/security/policy.json"))
	if err != nil {
		return err
	}
	registry, err := securitypolicy.LoadExceptions(filepath.Join(root, ".github/security/exceptions.json"))
	if err != nil {
		return err
	}
	if err := securitypolicy.ValidateExceptions(policy, registry, time.Now(), ""); err != nil {
		return err
	}
	npm := c.NPM
	if npm == "" {
		npm = "npm"
	}
	npm, err = exec.LookPath(npm)
	if err != nil {
		return fmt.Errorf("npm unavailable")
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	b, err := os.ReadFile(filepath.Join(root, "ui/package.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(b, &pkg) != nil || !strings.HasPrefix(pkg.PackageManager, "npm@") {
		return fmt.Errorf("missing pinned npm version")
	}
	npmVersion := strings.TrimPrefix(pkg.PackageManager, "npm@")
	var toolsPackage struct {
		PackageManager string `json:"packageManager"`
	}
	toolsData, err := os.ReadFile(filepath.Join(root, "tools/ci/package.json"))
	if err != nil {
		return err
	}
	if json.Unmarshal(toolsData, &toolsPackage) != nil || toolsPackage.PackageManager != pkg.PackageManager {
		return fmt.Errorf("npm pins disagree across application and CI tooling")
	}
	cache, err := os.MkdirTemp(c.Output, ".scanner-cache-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cache)
	// Public advisory lookups use no developer npm credentials or inherited config.
	goPathResult := execute(ctx, "go", []string{"env", "-json", "GOPATH", "GOROOT", "GOVERSION"}, root, os.Environ())
	if goPathResult.Exit != 0 {
		return fmt.Errorf("cannot resolve Go module cache")
	}
	userConfig := filepath.Join(cache, "npm-user-config")
	globalConfig := filepath.Join(cache, "npm-global-config")
	for _, path := range []string{userConfig, globalConfig} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return err
		}
	}
	var goEnv struct{ GOPATH, GOROOT, GOVERSION string }
	if json.Unmarshal(goPathResult.Output, &goEnv) != nil {
		return fmt.Errorf("invalid Go environment")
	}
	pinnedGo, err := os.ReadFile(filepath.Join(root, ".go-version"))
	if err != nil {
		return err
	}
	if goEnv.GOVERSION != "go"+strings.TrimSpace(string(pinnedGo)) {
		return fmt.Errorf("Go analysis toolchain differs from .go-version")
	}
	env := []string{
		"PATH=" + filepath.Join(goEnv.GOROOT, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + cache,
		"TMPDIR=" + os.TempDir(),
		"GOPATH=" + goEnv.GOPATH,
		"GOCACHE=" + filepath.Join(cache, "go-build"),
		"GOPROXY=https://proxy.golang.org",
		"GOSUMDB=sum.golang.org",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOTELEMETRY=off",
		"XDG_CACHE_HOME=" + cache,
		"NPM_CONFIG_USERCONFIG=" + userConfig,
		"NPM_CONFIG_GLOBALCONFIG=" + globalConfig,
		"NPM_CONFIG_CACHE=" + filepath.Join(cache, "npm"),
		"NPM_CONFIG_REGISTRY=https://registry.npmjs.org/",
		"NPM_CONFIG_FETCH_RETRIES=0",
		"NPM_CONFIG_FETCH_TIMEOUT=60000",
		"NPM_CONFIG_UPDATE_NOTIFIER=false",
		"NPM_CONFIG_AUDIT=true",
		"NPM_CONFIG_OMIT=",
		"NPM_CONFIG_IGNORE_SCRIPTS=true",
	}
	actual := execute(ctx, npm, []string{"--version"}, root, env)
	if actual.Exit != 0 || strings.TrimSpace(string(actual.Output)) != npmVersion {
		return fmt.Errorf("npm version differs from pinned packageManager")
	}
	govuln, err := filepath.Abs(c.Govulncheck)
	if err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(govuln)
	if err != nil || info.Path != "golang.org/x/vuln/cmd/govulncheck" || info.Main.Version != GovulncheckVersion || info.GoVersion != goEnv.GOVERSION {
		return fmt.Errorf("govulncheck build identity does not match pinned version")
	}
	npmArgs := []string{"audit", "--json", "--package-lock-only", "--include=dev", "--include=optional", "--include=peer", "--audit-level=low", "--ignore-scripts"}
	tracked := execute(ctx, "git", []string{"ls-files", "-z", "--", "*.go"}, root, env)
	if tracked.Exit != 0 {
		return fmt.Errorf("cannot inventory tracked Go source")
	}
	packageRoots, entryRoots := goRoots(strings.Split(string(tracked.Output), "\x00"))
	if len(packageRoots) == 0 || len(entryRoots) == 0 {
		return fmt.Errorf("missing first-party Go package or command roots")
	}
	goArgs := []string{"-json", "-mode=source", "-scan=symbol", "-db=https://vuln.go.dev"}
	specs := []scanSpec{
		{id: "npm-ui", scanner: "npm-audit", path: "ui/package-lock.json", dir: "ui", command: npm, version: npmVersion, scope: "complete application production/development graph", args: npmArgs},
		{id: "npm-tools", scanner: "npm-audit", path: "tools/ci/package-lock.json", dir: "tools/ci", command: npm, version: npmVersion, scope: "complete CI-tool production/development graph", args: npmArgs},
		{id: "go-packages", scanner: "govulncheck", path: "go.mod", dir: ".", command: govuln, version: GovulncheckVersion, scope: "all tracked first-party package graphs", args: append(append([]string{}, goArgs...), packageRoots...)},
		{id: "go-entrypoints", scanner: "govulncheck", path: "go.mod", dir: ".", command: govuln, version: GovulncheckVersion, scope: "all tracked command entry-point graphs", args: append(append([]string{}, goArgs...), entryRoots...)},
	}
	receipts := []receipt{}
	blocked := false
	for _, spec := range specs {
		o := Options{Context: c.Context, Revision: c.Revision, Path: spec.path, Now: time.Now().UTC()}
		lock, err := os.ReadFile(filepath.Join(root, spec.path))
		if err != nil {
			return fmt.Errorf("missing scan input %s", spec.path)
		}
		if spec.scanner == "govulncheck" {
			sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
			if err != nil {
				return err
			}
			lock = append(append(lock, 0), sums...)
		}
		r := receipt{ID: spec.id, Scanner: spec.scanner, ToolVersion: spec.version, Revision: c.Revision, InputSHA256: fmt.Sprintf("%x", sha256Bytes(lock)), Scope: spec.scope}
		var lastOutput []byte
		capture := func(ctx context.Context, name string, args []string, dir string, env []string) processResult {
			result := execute(ctx, name, args, dir, env)
			lastOutput = result.Output
			return result
		}
		report, attempts, exit := scan(ctx, c.Timeout, spec, o, lock, env, root, capture)
		r.Platform = runtime.GOOS + "/" + runtime.GOARCH
		if spec.scanner == "govulncheck" {
			var header struct {
				Config struct {
					GoVersion string `json:"go_version"`
					Modified  string `json:"db_last_modified"`
				} `json:"config"`
			}
			if json.NewDecoder(bytes.NewReader(lastOutput)).Decode(&header) == nil {
				if header.Config.GoVersion != goEnv.GOVERSION {
					report = baseReport(spec.scanner, o)
					report.Status = "invalid_report"
					report.Attempts = attempts
				} else {
					r.GoVersion = header.Config.GoVersion
				}
				if _, err := time.Parse(time.RFC3339, header.Config.Modified); err == nil {
					r.DatabaseModified = header.Config.Modified
				}
			}
		}
		after, err := os.ReadFile(filepath.Join(root, spec.path))
		if err != nil {
			return err
		}
		if spec.scanner == "govulncheck" {
			sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
			if err != nil {
				return err
			}
			after = append(append(after, 0), sums...)
		}
		if !bytes.Equal(after, lock) {
			report = baseReport(spec.scanner, o)
			report.Status = "invalid_report"
			report.Attempts = attempts
		}
		r.Status = report.Status
		r.Attempts = attempts
		r.ExitCode = exit
		report.ScanID += "-" + spec.id
		if err := writeJSON(filepath.Join(c.Output, spec.id+"-report.json"), report); err != nil {
			return err
		}
		decision, err := securitypolicy.Evaluate(policy, registry, report, time.Now())
		if err != nil {
			return fmt.Errorf("invalid dependency policy input: %w", err)
		}
		if err := writeJSON(filepath.Join(c.Output, spec.id+"-decision.json"), decision); err != nil {
			return err
		}
		r.Gate = decision.Gate
		receipts = append(receipts, r)
		fmt.Printf("%s: status=%s attempts=%d findings=%d gate=%s\n", spec.id, report.Status, attempts, len(report.Findings), decision.Gate)
		if decision.Gate != securitypolicy.GatePassed {
			blocked = true
		}
		if err := writeJSON(filepath.Join(c.Output, "manifest.json"), receipts); err != nil {
			return err
		}
	}
	if blocked {
		return fmt.Errorf("dependency security blocked or incomplete; inspect normalized reports")
	}
	return nil
}
func scan(ctx context.Context, timeout time.Duration, spec scanSpec, o Options, lock []byte, env []string, root string, run executor) (securitypolicy.FindingReport, int, int) {
	report := baseReport(spec.scanner, o)
	exit := -1
	for attempt := 1; attempt <= 2; attempt++ {
		child, cancel := context.WithTimeout(ctx, timeout)
		result := run(child, spec.command, spec.args, filepath.Join(root, spec.dir), env)
		cancel()
		exit = result.Exit
		status := failureStatus(spec.scanner, result)
		if status == "" {
			var err error
			if spec.scanner == "npm-audit" {
				report, err = NormalizeNPM(bytes.NewReader(result.Output), bytes.NewReader(lock), o)
			} else {
				report, err = NormalizeGo(bytes.NewReader(result.Output), o)
			}
			if err != nil {
				status = "invalid_report"
			} else {
				report.Attempts = attempt
				return report, attempt, exit
			}
		}
		report = baseReport(spec.scanner, o)
		report.Status = status
		report.Attempts = attempt
		if attempt == 2 || status != "timed_out" && status != "data_unavailable" || ctx.Err() != nil {
			return report, attempt, exit
		}
	}
	return report, report.Attempts, exit
}
func failureStatus(scanner string, r processResult) string {
	if r.TimedOut {
		return "timed_out"
	}
	if scanner == "npm-audit" {
		var response struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(r.Output, &response) == nil && response.Error != nil {
			switch response.Error.Code {
			case "ETIMEDOUT", "ENOTFOUND", "EAI_AGAIN", "ECONNRESET", "ECONNREFUSED", "E429", "E500", "E502", "E503", "E504":
				return "data_unavailable"
			default:
				return "failed"
			}
		}
		if r.Exit == 0 || r.Exit == 1 {
			return ""
		}
	} else if r.Exit == 0 {
		return ""
	}
	stderr := strings.ToLower(string(r.Stderr))
	for _, transient := range []string{"timeout", "timed out", "no such host", "connection refused", "connection reset", "status code 503", "status code 502", "status code 429"} {
		if strings.Contains(stderr, transient) {
			return "data_unavailable"
		}
	}
	return "failed"
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
func sha256Bytes(b []byte) [32]byte { return sha256.Sum256(b) }

var _ io.Writer = (*boundedBuffer)(nil)

// npm packages can contain auxiliary Go files. Derive roots from tracked
// first-party source so installing node_modules cannot change scan coverage.
func goRoots(files []string) ([]string, []string) {
	dirs := map[string]bool{}
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(file))
		skip := false
		for _, part := range strings.Split(dir, "/") {
			if part == "testdata" || part == "vendor" || part == "node_modules" || part != "." && (strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
				skip = true
			}
		}
		if !skip {
			if dir == "." {
				dirs["."] = true
			} else {
				dirs["./"+dir] = true
			}
		}
	}
	all, commands := []string{}, []string{}
	for dir := range dirs {
		all = append(all, dir)
		if strings.HasPrefix(dir, "./cmd/") || dir == "./cmd" {
			commands = append(commands, dir)
		}
	}
	sort.Strings(all)
	sort.Strings(commands)
	return all, commands
}
