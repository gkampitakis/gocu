package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/gkampitakis/gocu/internal/resolver"
)

func TestWriteTable_AllUpToDate(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0"},
	}, false, true)
	if !strings.Contains(buf.String(), "All dependencies up to date") {
		t.Errorf("expected up-to-date message, got %q", buf.String())
	}
}

func TestWriteTable_NoColor(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
		{Path: "ex.com/b", Current: "v2.0.0", Target: "v3.0.0", Bump: resolver.BumpMajor},
	}, false, true)

	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("no-color output should not contain ANSI escapes: %q", out)
	}
	if !strings.Contains(out, "ex.com/a") || !strings.Contains(out, "v1.1.0") {
		t.Errorf("missing module/version in output: %q", out)
	}
	if !strings.Contains(out, "2 upgrade(s) available") {
		t.Errorf("missing summary line: %q", out)
	}
}

func TestWriteTable_HintControlledByShowHint(t *testing.T) {
	results := []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
	}
	var hint, noHint bytes.Buffer
	WriteTable(&hint, results, false, true)
	WriteTable(&noHint, results, false, false)

	if !strings.Contains(hint.String(), "Run with -u") {
		t.Errorf("expected hint when showHint=true: %q", hint.String())
	}
	if strings.Contains(noHint.String(), "Run with -u") {
		t.Errorf("hint should be suppressed when showHint=false: %q", noHint.String())
	}
	if !strings.Contains(noHint.String(), "to apply") {
		t.Errorf("expected apply summary when showHint=false: %q", noHint.String())
	}
}

func TestWriteTable_MajorBumpsSortFirst(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "ex.com/patch", Current: "v1.0.0", Target: "v1.0.1", Bump: resolver.BumpPatch},
		{Path: "ex.com/major", Current: "v1.0.0", Target: "v2.0.0", Bump: resolver.BumpMajor},
		{Path: "ex.com/minor", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
	}, false, true)

	out := buf.String()
	iMajor := strings.Index(out, "ex.com/major")
	iMinor := strings.Index(out, "ex.com/minor")
	iPatch := strings.Index(out, "ex.com/patch")
	if iMajor >= iMinor || iMinor >= iPatch {
		t.Errorf("expected major < minor < patch ordering, got positions %d/%d/%d",
			iMajor, iMinor, iPatch)
	}
}

func TestWriteTable_ColorIncludesHyperlinkAndAnsi(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
	}, true, true)

	out := buf.String()
	if !strings.Contains(out, "\x1b]8;;https://pkg.go.dev/ex.com/a") {
		t.Errorf("expected OSC 8 hyperlink in colored output: %q", out)
	}
	if !strings.Contains(out, "\x1b[33m") { // yellow for minor
		t.Errorf("expected minor color escape: %q", out)
	}
}

func TestWriteTable_SkipsResultsWithoutUpgrade(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "ex.com/pinned", Current: "v1.0.0"}, // no Target → no upgrade
		{Path: "ex.com/upgrade", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
	}, false, true)

	if strings.Contains(buf.String(), "ex.com/pinned") {
		t.Errorf("pinned module should not appear in upgrade table: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "1 upgrade(s)") {
		t.Errorf("expected count to be 1: %q", buf.String())
	}
}

func TestHyperlink(t *testing.T) {
	got := Hyperlink("https://example.com", "click")
	want := "\x1b]8;;https://example.com\x1b\\click\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Hyperlink mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestPkgDevURL(t *testing.T) {
	if got := PkgDevURL("github.com/foo/bar"); got != "https://pkg.go.dev/github.com/foo/bar" {
		t.Errorf("PkgDevURL = %q", got)
	}
}

func TestWriteTable_AlignmentRespectsLongestPath(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{Path: "short", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
		{
			Path:    "this/is/a/much/longer/path",
			Current: "v1.0.0",
			Target:  "v1.1.0",
			Bump:    resolver.BumpMinor,
		},
	}, false, true)

	// Each non-summary line should have the arrow at the same column. Split
	// on raw newlines (TrimSpace would strip the leading space from the
	// first line only and skew byte offsets).
	lines := strings.Split(buf.String(), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 rows, got %d: %q", len(lines), buf.String())
	}
	a, b := strings.Index(lines[0], "→"), strings.Index(lines[1], "→")
	if a != b {
		t.Errorf("arrow misaligned: row 0 at %d (%q), row 1 at %d (%q)",
			a, lines[0], b, lines[1])
	}
}

func TestWriteTable_WithPublishedAt(t *testing.T) {
	// Make sure non-zero PublishedAt doesn't break rendering (it isn't shown
	// in the table currently, but the entry should still print cleanly).
	var buf bytes.Buffer
	WriteTable(&buf, []resolver.Result{
		{
			Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0",
			Bump:        resolver.BumpMinor,
			PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}, false, true)
	if !strings.Contains(buf.String(), "v1.1.0") {
		t.Errorf("table missing target version: %q", buf.String())
	}
}
