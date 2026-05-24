// Package resolver picks a single target version per module based on the
// requested target mode (latest, minor, patch, ...).
package resolver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/gkampitakis/gocu/internal/proxy"
)

// Target is the version selection strategy.
type Target int

const (
	TargetLatest   Target = iota // proxy's @latest (typically highest non-prerelease release)
	TargetGreatest               // highest semver, including prereleases when --pre
	TargetNewest                 // most-recently published
	TargetMinor                  // highest within current major
	TargetPatch                  // highest within current major.minor
)

// ParseTarget maps a flag string to a Target.
func ParseTarget(s string) (Target, error) {
	switch strings.ToLower(s) {
	case "latest":
		return TargetLatest, nil
	case "greatest":
		return TargetGreatest, nil
	case "newest":
		return TargetNewest, nil
	case "minor":
		return TargetMinor, nil
	case "patch":
		return TargetPatch, nil
	}
	return 0, fmt.Errorf("unknown target %q (want latest|greatest|newest|minor|patch)", s)
}

// Options configures resolution.
type Options struct {
	Target            Target
	IncludePrerelease bool
	AllowIncompatible bool
	// Cooldown skips candidates whose publish time is within this window of
	// Now. Zero disables the filter.
	Cooldown time.Duration
	// Now is the clock used for cooldown. Defaults to time.Now when nil.
	Now func() time.Time
}

// BumpKind is the classification of a version change.
type BumpKind int

const (
	BumpNone BumpKind = iota
	BumpPatch
	BumpMinor
	BumpMajor
)

func (b BumpKind) String() string {
	switch b {
	case BumpPatch:
		return "patch"
	case BumpMinor:
		return "minor"
	case BumpMajor:
		return "major"
	}
	return "none"
}

// Result is the resolver's per-module output.
type Result struct {
	Path        string
	Current     string
	Target      string // empty if no upgrade
	Bump        BumpKind
	PublishedAt time.Time // zero if unknown
}

// Resolve walks every known version of modulePath, applies opts, and returns
// the chosen target. If no version is newer than current the result has an
// empty Target and BumpNone.
func Resolve(
	ctx context.Context,
	c *proxy.Client,
	modulePath, current string,
	opts Options,
) (Result, error) {
	res := Result{Path: modulePath, Current: current}

	versions, err := c.List(ctx, modulePath)
	if err != nil {
		return res, err
	}

	candidates := filterCandidates(versions, current, opts)
	if len(candidates) == 0 {
		return res, nil
	}

	var chosen string
	var chosenTime time.Time
	switch {
	case opts.Target == TargetNewest:
		chosen, chosenTime = pickNewest(ctx, c, modulePath, candidates, opts)
	case opts.Cooldown > 0:
		chosen, chosenTime = pickHighestRespectingCooldown(ctx, c, modulePath, candidates, opts)
	default:
		chosen = pickHighest(candidates)
	}

	if chosen == "" || semver.Compare(chosen, current) <= 0 {
		return res, nil
	}

	res.Target = chosen
	res.Bump = classify(current, chosen)
	res.PublishedAt = chosenTime
	// If we haven't fetched Info yet (TargetLatest without cooldown), do a
	// best-effort fetch so the table can show timestamps.
	if res.PublishedAt.IsZero() {
		if info, err := c.Info(ctx, modulePath, chosen); err == nil {
			res.PublishedAt = info.Time
		}
	}
	return res, nil
}

// nowFn returns the configured clock or time.Now.
func nowFn(opts Options) func() time.Time {
	if opts.Now != nil {
		return opts.Now
	}
	return time.Now
}

func filterCandidates(versions []string, current string, opts Options) []string {
	currMajor := semver.Major(current)
	currMM := semver.MajorMinor(current)
	currentIsIncompat := strings.HasSuffix(current, "+incompatible")
	currentIsPre := semver.Prerelease(current) != ""

	out := make([]string, 0, len(versions))
	for _, v := range versions {
		if !semver.IsValid(v) {
			continue
		}
		if semver.Prerelease(v) != "" && !opts.IncludePrerelease && !currentIsPre {
			continue
		}
		if strings.HasSuffix(v, "+incompatible") && !opts.AllowIncompatible && !currentIsIncompat {
			continue
		}
		switch opts.Target {
		case TargetMinor:
			if semver.Major(v) != currMajor {
				continue
			}
		case TargetPatch:
			if semver.MajorMinor(v) != currMM {
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

func pickHighest(versions []string) string {
	best := ""
	for _, v := range versions {
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}

func pickNewest(
	ctx context.Context,
	c *proxy.Client,
	modulePath string,
	versions []string,
	opts Options,
) (string, time.Time) {
	now := nowFn(opts)
	var (
		bestVer  string
		bestTime time.Time
	)
	for _, v := range versions {
		info, err := c.Info(ctx, modulePath, v)
		if err != nil {
			continue
		}
		if opts.Cooldown > 0 && now().Sub(info.Time) < opts.Cooldown {
			continue
		}
		if bestVer == "" || info.Time.After(bestTime) {
			bestVer = v
			bestTime = info.Time
		}
	}
	return bestVer, bestTime
}

// pickHighestRespectingCooldown walks candidates from highest semver down,
// fetching each version's publish timestamp, and returns the first one
// outside the cooldown window. This minimizes proxy calls in the common case
// where most candidates are not too recent.
func pickHighestRespectingCooldown(
	ctx context.Context,
	c *proxy.Client,
	modulePath string,
	versions []string,
	opts Options,
) (string, time.Time) {
	sorted := make([]string, len(versions))
	copy(sorted, versions)
	sort.Slice(sorted, func(i, j int) bool {
		return semver.Compare(sorted[i], sorted[j]) > 0
	})

	now := nowFn(opts)
	for _, v := range sorted {
		info, err := c.Info(ctx, modulePath, v)
		if err != nil {
			// On info-fetch failure, fall back to accepting the version
			// rather than silently hiding upgrades.
			return v, time.Time{}
		}
		if now().Sub(info.Time) < opts.Cooldown {
			continue
		}
		return v, info.Time
	}
	return "", time.Time{}
}

func classify(from, to string) BumpKind {
	if semver.Major(from) != semver.Major(to) {
		return BumpMajor
	}
	if semver.MajorMinor(from) != semver.MajorMinor(to) {
		return BumpMinor
	}
	if semver.Canonical(from) != semver.Canonical(to) {
		return BumpPatch
	}
	return BumpNone
}
