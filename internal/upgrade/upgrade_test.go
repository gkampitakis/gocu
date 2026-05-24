package upgrade

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gkampitakis/gocu/internal/resolver"
)

func TestApply_DryRunPrintsCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	results := Apply(context.Background(), []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0"},
		{Path: "ex.com/b", Current: "v2.0.0", Target: "v2.0.5"},
	}, Options{DryRun: true, Tidy: true, Stdout: &stdout, Stderr: &stderr})

	out := stdout.String()
	if !strings.Contains(out, "go get ex.com/a@v1.1.0") {
		t.Errorf("missing first go get command: %q", out)
	}
	if !strings.Contains(out, "go get ex.com/b@v2.0.5") {
		t.Errorf("missing second go get command: %q", out)
	}
	if !strings.Contains(out, "go mod tidy") {
		t.Errorf("expected dry-run to print `go mod tidy`: %q", out)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("dry-run result should have nil Err, got %v", r.Err)
		}
	}
}

func TestApply_SkipsResultsWithoutTarget(t *testing.T) {
	var stdout bytes.Buffer
	results := Apply(context.Background(), []resolver.Result{
		{Path: "ex.com/pinned", Current: "v1.0.0"}, // no Target
		{Path: "ex.com/upgrade", Current: "v1.0.0", Target: "v1.1.0"},
	}, Options{DryRun: true, Stdout: &stdout})

	if strings.Contains(stdout.String(), "ex.com/pinned") {
		t.Errorf("pinned module should not be applied: %q", stdout.String())
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1 (only the upgrade)", len(results))
	}
}

func TestApply_DryRunSkipsTidyCommandWhenDisabled(t *testing.T) {
	var stdout bytes.Buffer
	Apply(context.Background(), []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0"},
	}, Options{DryRun: true, Tidy: false, Stdout: &stdout})

	if strings.Contains(stdout.String(), "go mod tidy") {
		t.Errorf("tidy=false should not print tidy command: %q", stdout.String())
	}
}
