// Package output formats resolver results for terminal display.
package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gkampitakis/gocu/internal/resolver"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	green  = "\x1b[32m"
	gray   = "\x1b[90m"
)

// WriteTable renders an aligned upgrade table to w. Results with no upgrade
// available are omitted. When showHint is true a trailing "run with -u" line
// is printed (suppressed when the caller is about to apply upgrades).
//
// When useColor is true, module paths are wrapped in OSC 8 hyperlinks pointing
// at pkg.go.dev. Terminals without OSC 8 support ignore the escape.
func WriteTable(w io.Writer, results []resolver.Result, useColor, showHint bool) {
	upgrades := make([]resolver.Result, 0, len(results))
	for _, r := range results {
		if r.Target != "" {
			upgrades = append(upgrades, r)
		}
	}
	if len(upgrades) == 0 {
		fmt.Fprintln(w, "All dependencies up to date.")
		return
	}

	sort.Slice(upgrades, func(i, j int) bool {
		if upgrades[i].Bump != upgrades[j].Bump {
			return upgrades[i].Bump > upgrades[j].Bump // major first
		}
		return upgrades[i].Path < upgrades[j].Path
	})

	// Width is computed from plain text so ANSI/OSC 8 escapes don't break
	// alignment.
	maxPath, maxCur, maxTgt := 0, 0, 0
	for _, r := range upgrades {
		if l := len(r.Path); l > maxPath {
			maxPath = l
		}
		if l := len(r.Current); l > maxCur {
			maxCur = l
		}
		if l := len(r.Target); l > maxTgt {
			maxTgt = l
		}
	}

	for _, r := range upgrades {
		path := r.Path
		target := r.Target
		if useColor {
			path = Hyperlink(PkgDevURL(r.Path), r.Path)
			target = colorize(r.Bump) + r.Target + reset
		}
		fmt.Fprintf(
			w, " %s%s  %s%s  → %s%s  %s\n",
			path, pad(maxPath-len(r.Path)),
			r.Current, pad(maxCur-len(r.Current)),
			target, pad(maxTgt-len(r.Target)),
			bumpLabel(r.Bump, useColor),
		)
	}

	if showHint {
		fmt.Fprintf(w, "\n%d upgrade(s) available. Run with -u to apply.\n", len(upgrades))
	} else {
		fmt.Fprintf(w, "\n%d upgrade(s) to apply:\n", len(upgrades))
	}
}

func pad(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func colorize(b resolver.BumpKind) string {
	switch b {
	case resolver.BumpMajor:
		return bold + red
	case resolver.BumpMinor:
		return yellow
	case resolver.BumpPatch:
		return green
	}
	return gray
}

func bumpLabel(b resolver.BumpKind, useColor bool) string {
	s := b.String()
	if useColor {
		return colorize(b) + s + reset
	}
	return s
}
