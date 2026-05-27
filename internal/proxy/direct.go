package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

// directBackend resolves versions for modules by talking directly to their
// source repo via git. It is used for "direct" entries in GOPROXY and for
// modules matching GOPRIVATE/GONOPROXY.
//
// List() runs `git ls-remote --tags` (cheap, no clone). Info() and Latest()
// require a local bare blobless clone so tag commit timestamps can be read;
// the clone is created lazily under the user cache dir and reused across runs.
type directBackend struct {
	runGit   func(ctx context.Context, dir string, args ...string) (string, error)
	cacheDir string // root for bare clones; empty → os.UserCacheDir()/gocu/git

	mu      sync.Mutex
	repos   map[string]*repoInfo       // modulePath → resolved git location
	listing map[string]*repoListing    // cloneURL → cached ls-remote result
	cloned  map[string]*repoTimestamps // cloneURL → cached tag timestamps
}

// repoInfo is the resolved git location for a module path.
type repoInfo struct {
	// CloneURL is the URL passed to `git clone`/`git ls-remote`.
	CloneURL string
	// Subdir is the in-repo path of the module, empty for root modules,
	// otherwise ending in "/" (e.g. "submod/" for github.com/foo/bar/submod).
	// Tags for this module are prefixed with Subdir.
	Subdir string
	// MajorSuffix is the /vN element of the module path for major versions
	// >= 2 (e.g. "v2", "v3"). Empty for v0/v1 modules. Tags must have this
	// major to belong to this module.
	MajorSuffix string
}

type repoListing struct {
	once sync.Once
	tags []string // raw tag names from ls-remote
	err  error
}

type repoTimestamps struct {
	once  sync.Once
	times map[string]time.Time // raw tag name → committer/tagger date
	err   error
}

func newDirectBackend() *directBackend {
	return &directBackend{
		runGit:  defaultRunGit,
		repos:   map[string]*repoInfo{},
		listing: map[string]*repoListing{},
		cloned:  map[string]*repoTimestamps{},
	}
}

func defaultRunGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return buf.String(), nil
}

// list returns the versions of modulePath as reported by `git ls-remote`.
func (d *directBackend) list(ctx context.Context, modulePath string) ([]string, error) {
	repo, err := d.resolveRepo(modulePath)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	l, ok := d.listing[repo.CloneURL]
	if !ok {
		l = &repoListing{}
		d.listing[repo.CloneURL] = l
	}
	d.mu.Unlock()

	l.once.Do(func() {
		out, gerr := d.runGit(ctx, "", "ls-remote", "--tags", "--refs", repo.CloneURL)
		if gerr != nil {
			l.err = gerr
			return
		}
		l.tags = parseLsRemoteTags(out)
	})
	if l.err != nil {
		return nil, l.err
	}
	return filterModuleVersions(l.tags, repo), nil
}

// info returns version+time for a single version, reading commit dates from
// a lazily-populated local bare clone.
func (d *directBackend) info(ctx context.Context, modulePath, version string) (Info, error) {
	repo, err := d.resolveRepo(modulePath)
	if err != nil {
		return Info{}, err
	}
	times, err := d.ensureTimestamps(ctx, repo.CloneURL)
	if err != nil {
		return Info{}, err
	}
	tag := repo.Subdir + version
	t, ok := times[tag]
	if !ok {
		return Info{}, ErrNotFound
	}
	return Info{Version: version, Time: t}, nil
}

// latest returns the highest non-prerelease version (or highest overall if all
// versions are prereleases) along with its timestamp.
func (d *directBackend) latest(ctx context.Context, modulePath string) (Info, error) {
	versions, err := d.list(ctx, modulePath)
	if err != nil {
		return Info{}, err
	}
	if len(versions) == 0 {
		return Info{}, ErrNotFound
	}
	best := pickLatestVersion(versions)
	if best == "" {
		return Info{}, ErrNotFound
	}
	return d.info(ctx, modulePath, best)
}

func pickLatestVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		if semver.Prerelease(v) != "" {
			continue
		}
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}
	if best != "" {
		return best
	}
	// fall back to highest overall, including prereleases
	for _, v := range versions {
		if best == "" || semver.Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}

func (d *directBackend) ensureTimestamps(
	ctx context.Context,
	cloneURL string,
) (map[string]time.Time, error) {
	d.mu.Lock()
	rt, ok := d.cloned[cloneURL]
	if !ok {
		rt = &repoTimestamps{}
		d.cloned[cloneURL] = rt
	}
	d.mu.Unlock()

	rt.once.Do(func() {
		rt.times, rt.err = d.populateTimestamps(ctx, cloneURL)
	})
	return rt.times, rt.err
}

func (d *directBackend) populateTimestamps(
	ctx context.Context,
	cloneURL string,
) (map[string]time.Time, error) {
	dir, err := d.repoCacheDir(cloneURL)
	if err != nil {
		return nil, err
	}

	if isExistingBareRepo(dir) {
		// Best-effort refresh; ignore errors and use what we have.
		_, _ = d.runGit(ctx, dir, "fetch", "--tags", "--prune", "--quiet")
	} else {
		// Wipe any partial state, then clone fresh.
		_ = os.RemoveAll(dir)
		if mkErr := os.MkdirAll(filepath.Dir(dir), 0o755); mkErr != nil {
			return nil, mkErr
		}
		if _, cloneErr := d.runGit(ctx, "", "clone", "--bare", "--filter=tree:0", "--quiet", cloneURL, dir); cloneErr != nil {
			return nil, cloneErr
		}
	}

	out, err := d.runGit(ctx, dir, "for-each-ref",
		"--format=%(refname:short)\t%(creatordate:iso-strict)",
		"refs/tags")
	if err != nil {
		return nil, err
	}
	return parseForEachRef(out), nil
}

func isExistingBareRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); err != nil {
		return false
	}
	return true
}

func (d *directBackend) repoCacheDir(cloneURL string) (string, error) {
	root := d.cacheDir
	if root == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(base, "gocu", "git")
	}
	sum := sha256.Sum256([]byte(cloneURL))
	return filepath.Join(root, hex.EncodeToString(sum[:8])+".git"), nil
}

// resolveRepo maps a module path to a repoInfo, using known-host fast paths.
// Returns an error for hosts that need go-import meta lookup (not yet
// implemented).
func (d *directBackend) resolveRepo(modulePath string) (*repoInfo, error) {
	d.mu.Lock()
	if r, ok := d.repos[modulePath]; ok {
		d.mu.Unlock()
		return r, nil
	}
	d.mu.Unlock()

	info, err := parseKnownHost(modulePath)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.repos[modulePath] = info
	d.mu.Unlock()
	return info, nil
}

var errUnsupportedHost = errors.New("direct fetch not supported for this host")

// parseKnownHost handles github.com, gitlab.com, and bitbucket.org module
// paths without HTTP. Returns errUnsupportedHost for other hosts.
func parseKnownHost(modulePath string) (*repoInfo, error) {
	parts := strings.Split(modulePath, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("module path %q has fewer than 3 segments", modulePath)
	}
	switch parts[0] {
	case "github.com", "gitlab.com", "bitbucket.org":
	default:
		return nil, fmt.Errorf("%w: %s (only github.com, gitlab.com, bitbucket.org supported)",
			errUnsupportedHost, parts[0])
	}

	repoSegs := parts[:3]
	rest := parts[3:]

	major := ""
	if n := len(rest); n > 0 && isMajorSuffix(rest[n-1]) {
		major = rest[n-1]
		rest = rest[:n-1]
	}

	subdir := ""
	if len(rest) > 0 {
		subdir = strings.Join(rest, "/") + "/"
	}

	return &repoInfo{
		CloneURL:    "https://" + strings.Join(repoSegs, "/") + ".git",
		Subdir:      subdir,
		MajorSuffix: major,
	}, nil
}

// isMajorSuffix returns true if s is "v2", "v3", etc. — a valid module-path
// major version suffix. v0 and v1 never appear as path elements.
func isMajorSuffix(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	if s == "v0" || s == "v1" {
		return false
	}
	return true
}

// parseLsRemoteTags parses `git ls-remote --tags --refs` output and returns
// the bare tag names. Annotated tag peelings (refs ending in "^{}") are
// stripped.
func parseLsRemoteTags(out string) []string {
	var tags []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		// format: "<sha>\trefs/tags/<tag>"
		_, after, ok := strings.Cut(line, "refs/tags/")
		if !ok {
			continue
		}
		tag := strings.TrimSuffix(after, "^{}")
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// parseForEachRef parses `git for-each-ref --format=%(refname:short)\t%(creatordate:iso-strict)`
// output into a tag → time map. Unparseable lines are skipped.
func parseForEachRef(out string) map[string]time.Time {
	times := map[string]time.Time{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		name, rawTime, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		raw := strings.TrimSpace(rawTime)
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			// Some git versions emit iso-strict without a colon in the offset.
			t, err = time.Parse("2006-01-02T15:04:05-0700", raw)
			if err != nil {
				continue
			}
		}
		times[name] = t
	}
	return times
}

// filterModuleVersions keeps only tags that map to valid module versions for
// the given repo (matching subdir + major-version constraints), returning
// canonical version strings.
func filterModuleVersions(tags []string, repo *repoInfo) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		v, ok := tagToVersion(t, repo)
		if !ok {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// tagToVersion converts a raw tag to a module version, or returns ok=false
// if it doesn't belong to this module.
func tagToVersion(tag string, repo *repoInfo) (string, bool) {
	if repo.Subdir != "" {
		if !strings.HasPrefix(tag, repo.Subdir) {
			return "", false
		}
		tag = tag[len(repo.Subdir):]
	} else if strings.Contains(tag, "/") {
		// Root module: tags containing "/" belong to nested modules.
		return "", false
	}
	if !semver.IsValid(tag) {
		return "", false
	}
	major := semver.Major(tag)
	if repo.MajorSuffix != "" {
		if major != repo.MajorSuffix {
			return "", false
		}
	} else if major != "v0" && major != "v1" {
		return "", false
	}
	return tag, true
}
