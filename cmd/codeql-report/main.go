package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/consize-oss/consize/internal/codeqlreport"
	"github.com/consize-oss/consize/internal/securitypolicy"
)

func main() {
	input := flag.String("input", "", "CodeQL SARIF file")
	output := flag.String("output", "", "Redacted normalized report")
	context := flag.String("context", "build", "Policy execution context")
	version := flag.String("version", "", "Scanned revision")
	flag.Parse()
	if err := run(*input, *output, *context, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(input, output, context, version string) error {
	if input == "" || output == "" || version == "" {
		return fmt.Errorf("input, output and version required")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	report, err := codeqlreport.Normalize(data, securitypolicy.ReportTarget{Component: "consize-source", Environment: "ci", Context: context, Version: version}, time.Now())
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(b, '\n'), 0o600)
}
