// Package upgrade applies resolved upgrades by shelling out to `go get`.
package upgrade

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/gkampitakis/gocu/internal/resolver"
)

// Options controls how Apply runs `go get`.
type Options struct {
	ModfileDir string // directory containing go.mod (cwd for `go get`)
	DryRun     bool   // print commands without executing
	Tidy       bool   // run `go mod tidy` after upgrades
	Stdout     io.Writer
	Stderr     io.Writer
}

// Result captures the per-module outcome of Apply.
type Result struct {
	Path    string
	Version string
	Err     error // non-nil if `go get` failed for this module
}

// Apply runs `go get module@version` for each upgrade. Failures don't abort
// the batch — each module's outcome is reported in the returned slice.
func Apply(ctx context.Context, upgrades []resolver.Result, opts Options) []Result {
	out := make([]Result, 0, len(upgrades))
	for _, u := range upgrades {
		if u.Target == "" {
			continue
		}
		r := Result{Path: u.Path, Version: u.Target}
		spec := u.Path + "@" + u.Target
		if opts.DryRun {
			fmt.Fprintf(opts.Stdout, "go get %s\n", spec)
			out = append(out, r)
			continue
		}
		r.Err = runCmd(ctx, opts, "go", "get", spec)
		if r.Err != nil {
			fmt.Fprintf(opts.Stderr, "gocu: go get %s: %v\n", spec, r.Err)
		}
		out = append(out, r)
	}

	if opts.Tidy && !opts.DryRun {
		if err := runCmd(ctx, opts, "go", "mod", "tidy"); err != nil {
			fmt.Fprintf(opts.Stderr, "gocu: go mod tidy: %v\n", err)
		}
	} else if opts.Tidy && opts.DryRun {
		fmt.Fprintln(opts.Stdout, "go mod tidy")
	}

	return out
}

func runCmd(ctx context.Context, opts Options, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	dir := opts.ModfileDir
	if dir == "" {
		dir = "."
	}
	cmd.Dir = filepath.Clean(dir)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	return cmd.Run()
}
