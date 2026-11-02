package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/consize-oss/consize/internal/dependencysecurity"
)

func main() {
	root := flag.String("root", ".", "Repository root")
	output := flag.String("output", "", "Normalized reports and decision directory")
	govuln := flag.String("govulncheck", "", "Pinned govulncheck executable")
	npm := flag.String("npm", "npm", "Pinned npm executable")
	revision := flag.String("revision", "", "Scanned Git revision")
	scanContext := flag.String("context", "build", "Policy execution context")
	timeout := flag.Duration("timeout", 4*time.Minute, "Timeout for each scanner attempt")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := dependencysecurity.Run(ctx, dependencysecurity.RunConfig{Root: *root, Output: *output, NPM: *npm, Govulncheck: *govuln, Revision: *revision, Context: *scanContext, Timeout: *timeout}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
