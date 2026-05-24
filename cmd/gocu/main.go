package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	gocli "github.com/gkampitakis/gocu/internal/cli"
	"github.com/gkampitakis/gocu/internal/cooldown"
	"github.com/gkampitakis/gocu/internal/resolver"
)

var version = "dev"

func main() {
	app := &cli.Command{
		Name:    "gocu",
		Usage:   "Go Check Updates — report and apply Go module upgrades",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "target", Aliases: []string{"t"}, Value: "latest",
				Usage: "version target: latest, greatest, newest, minor, patch",
			},
			&cli.StringSliceFlag{
				Name: "filter", Aliases: []string{"f"},
				Usage: "include only modules matching pattern (glob, /regex/, or comma list)",
			},
			&cli.StringSliceFlag{
				Name: "reject", Aliases: []string{"x"},
				Usage: "exclude modules matching pattern",
			},
			&cli.StringSliceFlag{
				Name:  "filter-version",
				Usage: "include only target versions matching pattern",
			},
			&cli.StringSliceFlag{
				Name:  "reject-version",
				Usage: "exclude target versions matching pattern",
			},
			&cli.StringFlag{
				Name: "dep", Value: "direct",
				Usage: "which deps to consider: direct, indirect, all",
			},
			&cli.BoolFlag{
				Name: "upgrade", Aliases: []string{"u"},
				Usage: "apply upgrades (runs `go get`)",
			},
			&cli.BoolFlag{
				Name: "interactive", Aliases: []string{"i"},
				Usage: "interactively pick upgrades to apply",
			},
			&cli.BoolFlag{
				Name: "deep", Aliases: []string{"d"},
				Usage: "walk for nested go.mod files",
			},
			&cli.BoolFlag{Name: "json", Usage: "emit JSON output"},
			&cli.StringFlag{
				Name:  "cooldown",
				Usage: "hide versions published within this window (e.g. 7d, 12h)",
			},
			&cli.BoolFlag{Name: "pre", Usage: "include prerelease versions"},
			&cli.BoolFlag{
				Name:  "allow-incompatible",
				Usage: "allow upgrading into +incompatible versions",
			},
			&cli.IntFlag{
				Name: "concurrency", Value: 8,
				Usage: "max parallel proxy requests",
			},
			&cli.BoolFlag{
				Name: "tidy", Value: true,
				Usage: "run `go mod tidy` after upgrades",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "with -u, print commands instead of executing",
			},
			&cli.StringFlag{
				Name:  "cwd",
				Usage: "operate in this directory instead of the current one",
			},
			&cli.BoolFlag{Name: "no-color", Usage: "disable colored output"},
		},
		Action: run,
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "gocu:", err)
		os.Exit(2)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	target, err := resolver.ParseTarget(cmd.String("target"))
	if err != nil {
		return err
	}

	cool, err := cooldown.ParseDuration(cmd.String("cooldown"))
	if err != nil {
		return err
	}

	dep := cmd.String("dep")
	var includeIndirect, onlyIndirect bool
	switch dep {
	case "direct":
		// defaults
	case "indirect":
		onlyIndirect = true
	case "all":
		includeIndirect = true
	default:
		return fmt.Errorf("invalid --dep %q (want direct|indirect|all)", dep)
	}

	useColor := !cmd.Bool("no-color") && term.IsTerminal(int(os.Stdout.Fd()))

	return gocli.Run(ctx, gocli.Options{
		Cwd:               cmd.String("cwd"),
		Target:            target,
		IncludePrerelease: cmd.Bool("pre"),
		AllowIncompatible: cmd.Bool("allow-incompatible"),
		Concurrency:       cmd.Int("concurrency"),
		IncludeIndirect:   includeIndirect,
		OnlyIndirect:      onlyIndirect,
		Filter:            cmd.StringSlice("filter"),
		Reject:            cmd.StringSlice("reject"),
		FilterVersion:     cmd.StringSlice("filter-version"),
		RejectVersion:     cmd.StringSlice("reject-version"),
		Upgrade:           cmd.Bool("upgrade"),
		DryRun:            cmd.Bool("dry-run"),
		Tidy:              cmd.Bool("tidy"),
		JSON:              cmd.Bool("json"),
		Interactive:       cmd.Bool("interactive"),
		Cooldown:          cool,
		UseColor:          useColor,
	})
}
