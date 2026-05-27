// Package proxy talks the Go module proxy protocol
// (https://proxy.golang.org/) to discover module versions. For "direct"
// entries in GOPROXY (and modules matched by GOPRIVATE/GONOPROXY) it falls
// back to a git-backed direct backend, see direct.go.
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
)

// ErrNotFound is returned when no proxy could serve the module/version.
var ErrNotFound = errors.New("not found")

// ErrPrivate is returned when a module matches GOPRIVATE/GONOPROXY and the
// configured GOPROXY has no "direct" entry to fall back on.
var ErrPrivate = errors.New(
	"module is private (GOPRIVATE/GONOPROXY) and no direct entry in GOPROXY",
)

// Info is the decoded response of {module}/@v/{version}.info or /@latest.
type Info struct {
	Version string
	Time    time.Time
}

// Client is a goroutine-safe GOPROXY client with an in-memory cache. It
// dispatches each lookup along the GOPROXY chain, using HTTP for normal
// entries and the git-backed direct backend for "direct" entries.
type Client struct {
	env    Env
	http   *http.Client
	direct *directBackend

	mu       sync.Mutex
	versions map[string][]string // module path -> versions list
	info     map[string]Info     // "module|key" -> info
	infoErr  map[string]error    // negative cache for info
}

// New returns a client using the provided env (or LoadEnv() when zero) and a
// default HTTP client with a 30s timeout.
func New(env Env) *Client {
	if len(env.Proxies) == 0 {
		env = LoadEnv()
	}
	return &Client{
		env:      env,
		http:     &http.Client{Timeout: 30 * time.Second},
		direct:   newDirectBackend(),
		versions: map[string][]string{},
		info:     map[string]Info{},
		infoErr:  map[string]error{},
	}
}

// List returns the list of known versions for modulePath. The slice is not
// sorted; callers should sort with semver.Compare as needed.
func (c *Client) List(ctx context.Context, modulePath string) ([]string, error) {
	c.mu.Lock()
	if v, ok := c.versions[modulePath]; ok {
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	versions, err := c.dispatchList(ctx, modulePath)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.versions[modulePath] = versions
	c.mu.Unlock()
	return versions, nil
}

// Latest returns the proxy's notion of "latest" for modulePath. For HTTP
// proxies this hits /@latest; for direct entries it picks the highest
// non-prerelease semver tag.
func (c *Client) Latest(ctx context.Context, modulePath string) (Info, error) {
	return c.cachedInfo(ctx, modulePath, "", "@latest")
}

// Info returns the version metadata for one specific version.
func (c *Client) Info(ctx context.Context, modulePath, version string) (Info, error) {
	return c.cachedInfo(ctx, modulePath, version, "info:"+version)
}

func (c *Client) cachedInfo(
	ctx context.Context,
	modulePath, version, cacheKey string,
) (Info, error) {
	key := modulePath + "|" + cacheKey
	c.mu.Lock()
	if i, ok := c.info[key]; ok {
		c.mu.Unlock()
		return i, nil
	}
	if e, ok := c.infoErr[key]; ok {
		c.mu.Unlock()
		return Info{}, e
	}
	c.mu.Unlock()

	info, err := c.dispatchInfo(ctx, modulePath, version)

	c.mu.Lock()
	if err != nil {
		c.infoErr[key] = err
	} else {
		c.info[key] = info
	}
	c.mu.Unlock()
	return info, err
}

// effectiveChain returns the GOPROXY entries usable for modulePath. For
// private modules, HTTP entries are filtered out (mirroring `go mod`'s
// behavior) so only "direct"/"off" survive.
func (c *Client) effectiveChain(modulePath string) []ProxyEntry {
	if !c.env.IsPrivate(modulePath) {
		return c.env.Proxies
	}
	out := make([]ProxyEntry, 0, len(c.env.Proxies))
	for _, p := range c.env.Proxies {
		if p.URL == "direct" || p.URL == "off" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Client) dispatchList(ctx context.Context, modulePath string) ([]string, error) {
	entries := c.effectiveChain(modulePath)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w; add ',direct' to GOPROXY to enable git fetch", ErrPrivate)
	}

	lastErr := ErrNotFound
	for _, p := range entries {
		switch p.URL {
		case "off":
			return nil, errors.New("GOPROXY=off")
		case "direct":
			versions, err := c.direct.list(ctx, modulePath)
			if err == nil {
				return versions, nil
			}
			lastErr = err
			if !p.FallbackOnNotFound {
				return nil, lastErr
			}
		default:
			versions, err := c.httpList(ctx, p, modulePath)
			if err == nil {
				return versions, nil
			}
			if errors.Is(err, ErrNotFound) {
				lastErr = ErrNotFound
				if !p.FallbackOnNotFound {
					return nil, ErrNotFound
				}
				continue
			}
			// Transport / 5xx error: comma falls through, pipe doesn't.
			lastErr = err
			if !p.FallbackOnNotFound {
				return nil, lastErr
			}
		}
	}
	return nil, lastErr
}

func (c *Client) dispatchInfo(ctx context.Context, modulePath, version string) (Info, error) {
	entries := c.effectiveChain(modulePath)
	if len(entries) == 0 {
		return Info{}, fmt.Errorf("%w; add ',direct' to GOPROXY to enable git fetch", ErrPrivate)
	}

	lastErr := ErrNotFound
	for _, p := range entries {
		switch p.URL {
		case "off":
			return Info{}, errors.New("GOPROXY=off")
		case "direct":
			info, err := c.directInfo(ctx, modulePath, version)
			if err == nil {
				return info, nil
			}
			lastErr = err
			if !p.FallbackOnNotFound {
				return Info{}, lastErr
			}
		default:
			info, err := c.httpInfo(ctx, p, modulePath, version)
			if err == nil {
				return info, nil
			}
			if errors.Is(err, ErrNotFound) {
				lastErr = ErrNotFound
				if !p.FallbackOnNotFound {
					return Info{}, ErrNotFound
				}
				continue
			}
			lastErr = err
			if !p.FallbackOnNotFound {
				return Info{}, lastErr
			}
		}
	}
	return Info{}, lastErr
}

func (c *Client) directInfo(ctx context.Context, modulePath, version string) (Info, error) {
	if version == "" {
		return c.direct.latest(ctx, modulePath)
	}
	return c.direct.info(ctx, modulePath, version)
}

func (c *Client) httpList(ctx context.Context, p ProxyEntry, modulePath string) ([]string, error) {
	esc, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("escape %q: %w", modulePath, err)
	}
	body, err := c.httpFetch(ctx, p, esc+"/@v/list")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	var versions []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			versions = append(versions, line)
		}
	}
	return versions, nil
}

func (c *Client) httpInfo(
	ctx context.Context,
	p ProxyEntry,
	modulePath, version string,
) (Info, error) {
	esc, err := module.EscapePath(modulePath)
	if err != nil {
		return Info{}, fmt.Errorf("escape %q: %w", modulePath, err)
	}
	var path string
	if version == "" {
		path = esc + "/@latest"
	} else {
		vesc, err := module.EscapeVersion(version)
		if err != nil {
			return Info{}, fmt.Errorf("escape version %q: %w", version, err)
		}
		path = esc + "/@v/" + vesc + ".info"
	}

	body, err := c.httpFetch(ctx, p, path)
	if err != nil {
		return Info{}, err
	}
	defer body.Close()

	var info Info
	if err := json.NewDecoder(body).Decode(&info); err != nil {
		return Info{}, fmt.Errorf("decode info: %w", err)
	}
	return info, nil
}

// httpFetch performs a single GET against one HTTP proxy entry, translating
// HTTP status into ErrNotFound (404/410) or a typed error.
func (c *Client) httpFetch(ctx context.Context, p ProxyEntry, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		strings.TrimRight(p.URL, "/")+"/"+path,
		nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case 200:
		return resp.Body, nil
	case 404, 410:
		resp.Body.Close()
		return nil, ErrNotFound
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("proxy %s: HTTP %d", p.URL, resp.StatusCode)
	}
}
