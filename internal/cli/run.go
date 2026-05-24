// Package cli orchestrates the end-to-end gocu workflow.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gkampitakis/gocu/internal/filter"
	"github.com/gkampitakis/gocu/internal/modfile"
	"github.com/gkampitakis/gocu/internal/output"
	"github.com/gkampitakis/gocu/internal/proxy"
	"github.com/gkampitakis/gocu/internal/resolver"
	"github.com/gkampitakis/gocu/internal/tui"
	"github.com/gkampitakis/gocu/internal/upgrade"
)

// Options holds the inputs needed for a single gocu invocation.
type Options struct {
	Cwd               string
	Target            resolver.Target
	IncludePrerelease bool
	AllowIncompatible bool
	Concurrency       int
	IncludeIndirect   bool
	OnlyIndirect      bool
	Filter            []string      // include only modules matching
	Reject            []string      // exclude modules matching
	FilterVersion     []string      // include only resolved targets matching
	RejectVersion     []string      // exclude resolved targets matching
	Upgrade           bool          // apply upgrades
	DryRun            bool          // with Upgrade: print commands only
	Tidy              bool          // with Upgrade: run `go mod tidy` after
	JSON              bool          // emit JSON instead of a table
	Interactive       bool          // open the TUI picker (implies Upgrade)
	Cooldown          time.Duration // skip versions published within this window
	Stdout            io.Writer
	Stderr            io.Writer
	UseColor          bool
}

// Run executes the report (and, in later phases, apply) pipeline.
func Run(ctx context.Context, opts Options) error {
	if opts.Cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.Cwd = wd
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}

	mfPath := filepath.Join(opts.Cwd, "go.mod")
	mf, err := modfile.Parse(mfPath)
	if err != nil {
		return err
	}

	nameInclude, err := filter.Compile(opts.Filter)
	if err != nil {
		return fmt.Errorf("--filter: %w", err)
	}
	nameExclude, err := filter.Compile(opts.Reject)
	if err != nil {
		return fmt.Errorf("--reject: %w", err)
	}
	verInclude, err := filter.Compile(opts.FilterVersion)
	if err != nil {
		return fmt.Errorf("--filter-version: %w", err)
	}
	verExclude, err := filter.Compile(opts.RejectVersion)
	if err != nil {
		return fmt.Errorf("--reject-version: %w", err)
	}

	mods := selectModules(mf.Modules(), opts, nameInclude, nameExclude)
	if len(mods) == 0 {
		fmt.Fprintln(opts.Stdout, "no dependencies to check")
		return nil
	}

	client := proxy.New(proxy.LoadEnv())
	resOpts := resolver.Options{
		Target:            opts.Target,
		IncludePrerelease: opts.IncludePrerelease,
		AllowIncompatible: opts.AllowIncompatible,
		Cooldown:          opts.Cooldown,
	}

	results := resolveAll(ctx, client, mods, resOpts, opts.Concurrency, opts.Stderr)
	results = filterByVersion(results, verInclude, verExclude)

	if opts.Interactive {
		selected, confirmed, err := tui.Pick(results)
		if err != nil {
			return err
		}
		if !confirmed || len(selected) == 0 {
			fmt.Fprintln(opts.Stdout, "no upgrades applied.")
			return nil
		}
		return applyAndReport(ctx, selected, opts)
	}

	if opts.JSON {
		if err := output.WriteJSON(opts.Stdout, results); err != nil {
			return err
		}
	} else {
		output.WriteTable(opts.Stdout, results, opts.UseColor, !opts.Upgrade)
	}

	if opts.Upgrade {
		return applyAndReport(ctx, results, opts)
	}
	return nil
}

func applyAndReport(ctx context.Context, results []resolver.Result, opts Options) error {
	applied := upgrade.Apply(ctx, results, upgrade.Options{
		ModfileDir: opts.Cwd,
		DryRun:     opts.DryRun,
		Tidy:       opts.Tidy,
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
	})
	failed := 0
	for _, r := range applied {
		if r.Err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d upgrade(s) failed", failed)
	}
	return nil
}

func filterByVersion(
	results []resolver.Result,
	include, exclude *filter.Matcher,
) []resolver.Result {
	if include.Empty() && exclude.Empty() {
		return results
	}
	out := make([]resolver.Result, 0, len(results))
	for _, r := range results {
		if r.Target == "" {
			out = append(out, r)
			continue
		}
		if !include.Empty() && !include.Match(r.Target) {
			continue
		}
		if exclude.Match(r.Target) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func selectModules(
	all []modfile.Module,
	opts Options,
	include, exclude *filter.Matcher,
) []modfile.Module {
	out := make([]modfile.Module, 0, len(all))
	for _, m := range all {
		if m.Replaced {
			continue
		}
		switch {
		case opts.OnlyIndirect && !m.Indirect:
			continue
		case !opts.IncludeIndirect && !opts.OnlyIndirect && m.Indirect:
			continue
		}
		if !include.Empty() && !include.Match(m.Path) {
			continue
		}
		if exclude.Match(m.Path) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func resolveAll(ctx context.Context, c *proxy.Client, mods []modfile.Module,
	resOpts resolver.Options, concurrency int, stderr io.Writer,
) []resolver.Result {
	results := make([]resolver.Result, len(mods))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, m := range mods {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, m modfile.Module) {
			defer wg.Done()
			defer func() { <-sem }()

			r, err := resolver.Resolve(ctx, c, m.Path, m.Version, resOpts)
			if err != nil {
				fmt.Fprintf(stderr, "gocu: %s: %v\n", m.Path, err)
				results[i] = resolver.Result{Path: m.Path, Current: m.Version}
				return
			}
			results[i] = r
		}(i, m)
	}
	wg.Wait()
	return results
}
