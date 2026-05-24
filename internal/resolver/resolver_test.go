package resolver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gkampitakis/gocu/internal/proxy"
)

func fakeProxy(t *testing.T, versions []string, times map[string]string) *proxy.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/@v/list"):
			fmt.Fprintln(w, strings.Join(versions, "\n"))
		case strings.HasSuffix(r.URL.Path, ".info"):
			parts := strings.Split(r.URL.Path, "/")
			ver := strings.TrimSuffix(parts[len(parts)-1], ".info")
			ts := times[ver]
			if ts == "" {
				ts = "2024-01-01T00:00:00Z"
			}
			fmt.Fprintf(w, `{"Version":%q,"Time":%q}`, ver, ts)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return proxy.New(
		proxy.Env{Proxies: []proxy.ProxyEntry{{URL: srv.URL, FallbackOnNotFound: true}}},
	)
}

func TestResolve_Latest(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0"}, nil)
	r, err := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{Target: TargetLatest})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "v2.0.0" {
		t.Errorf("Target = %q, want v2.0.0", r.Target)
	}
	if r.Bump != BumpMajor {
		t.Errorf("Bump = %v, want major", r.Bump)
	}
}

func TestResolve_Minor(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0", "v1.2.0", "v2.0.0"}, nil)
	r, err := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{Target: TargetMinor})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "v1.2.0" {
		t.Errorf("Target = %q, want v1.2.0", r.Target)
	}
	if r.Bump != BumpMinor {
		t.Errorf("Bump = %v, want minor", r.Bump)
	}
}

func TestResolve_Patch(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.0.5", "v1.1.0"}, nil)
	r, err := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{Target: TargetPatch})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "v1.0.5" {
		t.Errorf("Target = %q, want v1.0.5", r.Target)
	}
	if r.Bump != BumpPatch {
		t.Errorf("Bump = %v, want patch", r.Bump)
	}
}

func TestResolve_NoUpgrade(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0"}, nil)
	r, err := Resolve(context.Background(), c, "ex.com/m", "v1.1.0", Options{Target: TargetLatest})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "" {
		t.Errorf("Target = %q, want empty", r.Target)
	}
	if r.Bump != BumpNone {
		t.Errorf("Bump = %v, want none", r.Bump)
	}
}

func TestResolve_SkipsPrereleaseByDefault(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0", "v2.0.0-beta.1"}, nil)
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{Target: TargetLatest})
	if r.Target != "v1.1.0" {
		t.Errorf("Target = %q, want v1.1.0 (prerelease skipped)", r.Target)
	}
}

func TestResolve_IncludePrerelease(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0", "v2.0.0-beta.1"}, nil)
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v1.0.0",
		Options{Target: TargetGreatest, IncludePrerelease: true})
	if r.Target != "v2.0.0-beta.1" {
		t.Errorf("Target = %q, want v2.0.0-beta.1", r.Target)
	}
}

func TestResolve_PrereleaseAutoOnIfCurrentIsPre(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0-rc1", "v1.0.0-rc2"}, nil)
	r, _ := Resolve(
		context.Background(),
		c,
		"ex.com/m",
		"v1.0.0-rc1",
		Options{Target: TargetGreatest},
	)
	if r.Target != "v1.0.0-rc2" {
		t.Errorf("Target = %q, want v1.0.0-rc2", r.Target)
	}
}

func TestResolve_SkipsIncompatibleByDefault(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v2.0.0+incompatible"}, nil)
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{Target: TargetLatest})
	if r.Target != "" {
		t.Errorf("Target = %q, want empty (+incompatible skipped)", r.Target)
	}
}

func TestResolve_AllowIncompatible(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v2.0.0+incompatible"}, nil)
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v1.0.0",
		Options{Target: TargetLatest, AllowIncompatible: true})
	if r.Target != "v2.0.0+incompatible" {
		t.Errorf("Target = %q, want v2.0.0+incompatible", r.Target)
	}
}

func TestResolve_Newest(t *testing.T) {
	// v1.0.0 published most recently, even though v2 has higher semver.
	c := fakeProxy(t, []string{"v1.0.0", "v2.0.0"}, map[string]string{
		"v1.0.0": "2024-12-01T00:00:00Z",
		"v2.0.0": "2024-01-01T00:00:00Z",
	})
	r, err := Resolve(context.Background(), c, "ex.com/m", "v0.9.0", Options{Target: TargetNewest})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "v1.0.0" {
		t.Errorf("Target = %q, want v1.0.0 (newest)", r.Target)
	}
}

func TestResolve_CooldownSkipsTooRecent(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0", "v1.2.0"}, map[string]string{
		"v1.0.0": "2024-01-01T00:00:00Z",
		"v1.1.0": "2024-06-01T00:00:00Z",
		"v1.2.0": "2024-12-09T00:00:00Z", // very recent vs. "now"
	})
	now := func() time.Time { return time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC) }
	r, err := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{
		Target:   TargetLatest,
		Cooldown: 7 * 24 * time.Hour,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != "v1.1.0" {
		t.Errorf("Target = %q, want v1.1.0 (v1.2.0 inside cooldown)", r.Target)
	}
}

func TestResolve_CooldownAcceptsFreshEnough(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0"}, map[string]string{
		"v1.0.0": "2024-01-01T00:00:00Z",
		"v1.1.0": "2024-11-01T00:00:00Z", // older than the cooldown window
	})
	now := func() time.Time { return time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC) }
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v1.0.0", Options{
		Target:   TargetLatest,
		Cooldown: 7 * 24 * time.Hour,
		Now:      now,
	})
	if r.Target != "v1.1.0" {
		t.Errorf("Target = %q, want v1.1.0", r.Target)
	}
	if r.PublishedAt.IsZero() {
		t.Error("PublishedAt should be set")
	}
}

func TestResolve_CooldownHidesAllUpgrades(t *testing.T) {
	c := fakeProxy(t, []string{"v1.0.0", "v1.1.0"}, map[string]string{
		"v1.0.0": "2024-12-09T00:00:00Z",
		"v1.1.0": "2024-12-09T00:00:00Z",
	})
	now := func() time.Time { return time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC) }
	r, _ := Resolve(context.Background(), c, "ex.com/m", "v0.9.0", Options{
		Target:   TargetLatest,
		Cooldown: 7 * 24 * time.Hour,
		Now:      now,
	})
	if r.Target != "" {
		t.Errorf("Target = %q, want empty (all candidates inside cooldown)", r.Target)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		from, to string
		want     BumpKind
	}{
		{"v1.0.0", "v2.0.0", BumpMajor},
		{"v1.0.0", "v1.1.0", BumpMinor},
		{"v1.0.0", "v1.0.1", BumpPatch},
		{"v1.0.0", "v1.0.0", BumpNone},
	}
	for _, tc := range cases {
		if got := classify(tc.from, tc.to); got != tc.want {
			t.Errorf("classify(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
