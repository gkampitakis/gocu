package proxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseKnownHost(t *testing.T) {
	cases := []struct {
		in   string
		want *repoInfo
	}{
		{
			"github.com/foo/bar",
			&repoInfo{CloneURL: "https://github.com/foo/bar.git"},
		},
		{
			"github.com/foo/bar/v2",
			&repoInfo{CloneURL: "https://github.com/foo/bar.git", MajorSuffix: "v2"},
		},
		{
			"github.com/foo/bar/sub",
			&repoInfo{CloneURL: "https://github.com/foo/bar.git", Subdir: "sub/"},
		},
		{
			"github.com/foo/bar/sub/v3",
			&repoInfo{
				CloneURL:    "https://github.com/foo/bar.git",
				Subdir:      "sub/",
				MajorSuffix: "v3",
			},
		},
		{
			"github.com/foo/bar/deep/nested",
			&repoInfo{CloneURL: "https://github.com/foo/bar.git", Subdir: "deep/nested/"},
		},
		{
			"gitlab.com/group/repo",
			&repoInfo{CloneURL: "https://gitlab.com/group/repo.git"},
		},
		{
			"bitbucket.org/owner/repo/v4",
			&repoInfo{CloneURL: "https://bitbucket.org/owner/repo.git", MajorSuffix: "v4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseKnownHost(tc.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseKnownHost_Unsupported(t *testing.T) {
	cases := []string{
		"example.com/foo/bar",
		"golang.org/x/sync",
		"k8s.io/client-go/api",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := parseKnownHost(in)
			if !errors.Is(err, errUnsupportedHost) {
				t.Errorf("err = %v, want errUnsupportedHost", err)
			}
		})
	}
}

func TestParseKnownHost_TooShort(t *testing.T) {
	if _, err := parseKnownHost("github.com/foo"); err == nil {
		t.Error("expected error for path with fewer than 3 segments")
	}
}

func TestIsMajorSuffix(t *testing.T) {
	cases := map[string]bool{
		"v2":   true,
		"v3":   true,
		"v10":  true,
		"v0":   false,
		"v1":   false,
		"v":    false,
		"":     false,
		"foo":  false,
		"v2.0": false,
		"V2":   false,
	}
	for in, want := range cases {
		if got := isMajorSuffix(in); got != want {
			t.Errorf("isMajorSuffix(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTagToVersion(t *testing.T) {
	root := &repoInfo{CloneURL: "https://x", Subdir: "", MajorSuffix: ""}
	v2 := &repoInfo{CloneURL: "https://x", Subdir: "", MajorSuffix: "v2"}
	sub := &repoInfo{CloneURL: "https://x", Subdir: "sub/", MajorSuffix: ""}
	subV3 := &repoInfo{CloneURL: "https://x", Subdir: "sub/", MajorSuffix: "v3"}

	cases := []struct {
		repo *repoInfo
		tag  string
		ver  string
		ok   bool
	}{
		// root module: v0/v1 ok, v2+ filtered out
		{root, "v1.0.0", "v1.0.0", true},
		{root, "v0.5.0", "v0.5.0", true},
		{root, "v2.0.0", "", false},
		{root, "v1.0.0-rc1", "v1.0.0-rc1", true},
		{root, "sub/v1.0.0", "", false}, // nested module's tag
		{root, "garbage", "", false},

		// v2 module: only v2.x.y
		{v2, "v2.0.0", "v2.0.0", true},
		{v2, "v2.3.1", "v2.3.1", true},
		{v2, "v1.0.0", "", false},
		{v2, "v3.0.0", "", false},

		// subdir module: subdir/vN.M.P, default v0/v1 filter
		{sub, "sub/v1.0.0", "v1.0.0", true},
		{sub, "v1.0.0", "", false},          // missing prefix
		{sub, "other/v1.0.0", "", false},    // wrong prefix
		{sub, "sub/v2.0.0", "", false},      // wrong major
		{sub, "subother/v1.0.0", "", false}, // partial prefix match must be exact

		// subdir + v3 major
		{subV3, "sub/v3.0.0", "v3.0.0", true},
		{subV3, "sub/v2.0.0", "", false},
		{subV3, "v3.0.0", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag+"|"+tc.repo.Subdir+"|"+tc.repo.MajorSuffix, func(t *testing.T) {
			got, ok := tagToVersion(tc.tag, tc.repo)
			if ok != tc.ok || got != tc.ver {
				t.Errorf("tagToVersion(%q) = (%q,%v), want (%q,%v)",
					tc.tag, got, ok, tc.ver, tc.ok)
			}
		})
	}
}

func TestParseLsRemoteTags(t *testing.T) {
	in := strings.Join([]string{
		"abc123\trefs/tags/v1.0.0",
		"def456\trefs/tags/v1.1.0",
		"ghi789\trefs/tags/v1.1.0^{}", // peeled annotated tag — duplicate to be stripped
		"jkl000\trefs/tags/sub/v2.0.0",
		"",                     // blank line
		"xxx\trefs/heads/main", // non-tag ref, should be ignored
	}, "\n")
	got := parseLsRemoteTags(in)
	want := []string{"v1.0.0", "v1.1.0", "v1.1.0", "sub/v2.0.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseForEachRef(t *testing.T) {
	in := strings.Join([]string{
		"v1.0.0\t2024-01-15T10:00:00+00:00",
		"v1.1.0\t2024-06-01T12:30:45+02:00",
		"sub/v2.0.0\t2024-11-01T00:00:00-07:00",
		"bad-line-no-tab",
		"unparseable\tnot-a-date",
	}, "\n")
	got := parseForEachRef(in)

	want := map[string]time.Time{
		"v1.0.0":     time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		"v1.1.0":     time.Date(2024, 6, 1, 10, 30, 45, 0, time.UTC), // +02:00 normalized
		"sub/v2.0.0": time.Date(2024, 11, 1, 7, 0, 0, 0, time.UTC),   // -07:00 normalized
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing %q", k)
			continue
		}
		if !g.Equal(w) {
			t.Errorf("%q: got %v, want %v", k, g, w)
		}
	}
}

func TestFilterModuleVersions_Dedup(t *testing.T) {
	repo := &repoInfo{CloneURL: "x"}
	tags := []string{"v1.0.0", "v1.0.0", "v1.1.0", "v2.0.0", "sub/v1.0.0"}
	got := filterModuleVersions(tags, repo)
	want := []string{"v1.0.0", "v1.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPickLatestVersion(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"v1.0.0", "v1.1.0", "v2.0.0"}, "v2.0.0"},
		{[]string{"v1.0.0", "v2.0.0-rc1"}, "v1.0.0"},         // prerelease skipped
		{[]string{"v1.0.0-rc1", "v1.0.0-rc2"}, "v1.0.0-rc2"}, // all prereleases → highest
		{[]string{}, ""},
	}
	for _, tc := range cases {
		if got := pickLatestVersion(tc.in); got != tc.want {
			t.Errorf("pickLatestVersion(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// fakeGit records every invocation and dispatches to a per-args response map.
type fakeGit struct {
	t        *testing.T
	calls    []string
	response map[string]fakeGitResp
}

type fakeGitResp struct {
	out string
	err error
}

func (f *fakeGit) run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	for k, v := range f.response {
		if strings.HasPrefix(key, k) {
			return v.out, v.err
		}
	}
	f.t.Fatalf("fakeGit: unexpected call %q", key)
	return "", nil
}

func TestDirectBackend_List(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"ls-remote --tags --refs https://github.com/foo/bar.git": {
				out: "abc\trefs/tags/v1.0.0\ndef\trefs/tags/v1.1.0\nghi\trefs/tags/v2.0.0\n",
			},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run

	got, err := d.list(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.0.0", "v1.1.0"} // v2.0.0 filtered (wrong major for root v1 module)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Second call: same modulePath → cached, no extra git invocation.
	if _, err := d.list(context.Background(), "github.com/foo/bar"); err != nil {
		t.Fatal(err)
	}
	if len(fg.calls) != 1 {
		t.Errorf("expected ls-remote to run once (cached), got %d calls", len(fg.calls))
	}
}

func TestDirectBackend_List_V2Module(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"ls-remote --tags --refs https://github.com/foo/bar.git": {
				out: "a\trefs/tags/v1.0.0\nb\trefs/tags/v2.0.0\nc\trefs/tags/v2.1.0\n",
			},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run

	got, err := d.list(context.Background(), "github.com/foo/bar/v2")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v2.0.0", "v2.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDirectBackend_List_Subdir(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"ls-remote --tags --refs https://github.com/foo/bar.git": {
				out: strings.Join([]string{
					"a\trefs/tags/v1.0.0",
					"b\trefs/tags/sub/v1.0.0",
					"c\trefs/tags/sub/v1.1.0",
					"d\trefs/tags/other/v9.0.0",
				}, "\n"),
			},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run

	got, err := d.list(context.Background(), "github.com/foo/bar/sub")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.0.0", "v1.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDirectBackend_List_GitError(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"ls-remote": {err: errors.New("fatal: could not read from remote")},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run

	_, err := d.list(context.Background(), "github.com/foo/bar")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDirectBackend_List_UnsupportedHost(t *testing.T) {
	d := newDirectBackend()
	d.runGit = func(context.Context, string, ...string) (string, error) {
		t.Fatal("git should not be called for unsupported host")
		return "", nil
	}
	_, err := d.list(context.Background(), "example.com/foo/bar")
	if !errors.Is(err, errUnsupportedHost) {
		t.Errorf("err = %v, want errUnsupportedHost", err)
	}
}

// TestDirectBackend_Info exercises the lazy-clone + for-each-ref path using
// a fake git runner and an injected cacheDir to avoid touching the user's
// real cache.
func TestDirectBackend_Info(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"clone --bare --filter=tree:0 --quiet https://github.com/foo/bar.git": {},
			"for-each-ref": {
				out: strings.Join([]string{
					"v1.0.0\t2024-01-01T00:00:00+00:00",
					"v1.1.0\t2024-06-15T12:00:00+00:00",
				}, "\n"),
			},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run
	d.cacheDir = t.TempDir()

	info, err := d.info(context.Background(), "github.com/foo/bar", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.1.0" {
		t.Errorf("Version = %q, want v1.1.0", info.Version)
	}
	want := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	if !info.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", info.Time, want)
	}

	// Subsequent info call on same repo should not re-clone.
	cloneCount := 0
	for _, c := range fg.calls {
		if strings.HasPrefix(c, "clone ") {
			cloneCount++
		}
	}
	if _, err := d.info(context.Background(), "github.com/foo/bar", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	afterClones := 0
	for _, c := range fg.calls {
		if strings.HasPrefix(c, "clone ") {
			afterClones++
		}
	}
	if afterClones != cloneCount {
		t.Errorf("expected clone to run once, ran %d times total", afterClones)
	}
}

func TestDirectBackend_Info_RefreshExisting(t *testing.T) {
	// Pre-populate a fake bare repo on disk so populateTimestamps takes the
	// fetch path instead of clone.
	tmp := t.TempDir()
	d := newDirectBackend()
	d.cacheDir = tmp

	repoDir, err := d.repoCacheDir("https://github.com/foo/bar.git")
	if err != nil {
		t.Fatal(err)
	}
	// Create the marker files so isExistingBareRepo returns true.
	if err := writeFakeBareRepo(repoDir); err != nil {
		t.Fatal(err)
	}

	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"fetch --tags --prune --quiet": {},
			"for-each-ref": {
				out: "v1.0.0\t2024-01-01T00:00:00+00:00\n",
			},
		},
	}
	d.runGit = fg.run

	info, err := d.info(context.Background(), "github.com/foo/bar", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("Version = %q, want v1.0.0", info.Version)
	}
	for _, c := range fg.calls {
		if strings.HasPrefix(c, "clone ") {
			t.Errorf("expected no clone call, got: %v", fg.calls)
		}
	}
}

func TestDirectBackend_Latest(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"ls-remote": {
				out: strings.Join([]string{
					"a\trefs/tags/v1.0.0",
					"b\trefs/tags/v1.1.0",
					"c\trefs/tags/v1.2.0-rc1",
				}, "\n"),
			},
			"clone":        {},
			"for-each-ref": {out: "v1.1.0\t2024-06-15T12:00:00+00:00\n"},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run
	d.cacheDir = t.TempDir()

	info, err := d.latest(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.1.0" {
		t.Errorf("Version = %q, want v1.1.0 (prerelease skipped)", info.Version)
	}
}

func TestDirectBackend_Info_UnknownVersion(t *testing.T) {
	fg := &fakeGit{
		t: t,
		response: map[string]fakeGitResp{
			"clone":        {},
			"for-each-ref": {out: "v1.0.0\t2024-01-01T00:00:00+00:00\n"},
		},
	}
	d := newDirectBackend()
	d.runGit = fg.run
	d.cacheDir = t.TempDir()

	_, err := d.info(context.Background(), "github.com/foo/bar", "v9.9.9")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func writeFakeBareRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"HEAD", "config"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			return err
		}
	}
	return nil
}
