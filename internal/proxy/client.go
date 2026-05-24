// Package proxy talks the Go module proxy protocol
// (https://proxy.golang.org/) to discover module versions.
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
// caller asked the proxy to fetch it anyway.
var ErrPrivate = errors.New("module is private (GOPRIVATE/GONOPROXY)")

// Info is the decoded response of {module}/@v/{version}.info or /@latest.
type Info struct {
	Version string
	Time    time.Time
}

// Client is a goroutine-safe GOPROXY HTTP client with an in-memory cache.
type Client struct {
	env  Env
	http *http.Client

	mu       sync.Mutex
	versions map[string][]string // module path -> versions list
	info     map[string]Info     // "module@version" -> info
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

	if c.env.IsPrivate(modulePath) {
		return nil, ErrPrivate
	}

	esc, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("escape %q: %w", modulePath, err)
	}

	body, err := c.fetch(ctx, esc+"/@v/list")
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

	c.mu.Lock()
	c.versions[modulePath] = versions
	c.mu.Unlock()
	return versions, nil
}

// Latest returns the proxy's notion of "latest" for modulePath. This usually
// resolves to the highest semver release tag, with the proxy applying its own
// rules for prereleases and incompatible versions.
func (c *Client) Latest(ctx context.Context, modulePath string) (Info, error) {
	return c.fetchInfo(ctx, modulePath, "@latest")
}

// Info returns the version metadata for one specific version.
func (c *Client) Info(ctx context.Context, modulePath, version string) (Info, error) {
	esc, err := module.EscapeVersion(version)
	if err != nil {
		return Info{}, fmt.Errorf("escape version %q: %w", version, err)
	}
	return c.fetchInfo(ctx, modulePath, "@v/"+esc+".info")
}

func (c *Client) fetchInfo(ctx context.Context, modulePath, suffix string) (Info, error) {
	key := modulePath + "|" + suffix
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

	if c.env.IsPrivate(modulePath) {
		return Info{}, ErrPrivate
	}

	esc, err := module.EscapePath(modulePath)
	if err != nil {
		return Info{}, fmt.Errorf("escape %q: %w", modulePath, err)
	}

	body, err := c.fetch(ctx, esc+"/"+suffix)
	if err != nil {
		c.mu.Lock()
		c.infoErr[key] = err
		c.mu.Unlock()
		return Info{}, err
	}
	defer body.Close()

	var info Info
	if err := json.NewDecoder(body).Decode(&info); err != nil {
		return Info{}, fmt.Errorf("decode info: %w", err)
	}

	c.mu.Lock()
	c.info[key] = info
	c.mu.Unlock()
	return info, nil
}

// fetch walks the GOPROXY chain trying each entry until one returns the path
// or all are exhausted. "direct" and "off" entries terminate the chain.
func (c *Client) fetch(ctx context.Context, path string) (io.ReadCloser, error) {
	lastErr := ErrNotFound
	for _, p := range c.env.Proxies {
		switch p.URL {
		case "off":
			return nil, errors.New("GOPROXY=off")
		case "direct":
			return nil, errors.New("GOPROXY=direct not supported (proxy required)")
		}

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
			lastErr = err
			continue // transport error -> always fall through
		}
		switch resp.StatusCode {
		case 200:
			return resp.Body, nil
		case 404, 410:
			resp.Body.Close()
			lastErr = ErrNotFound
			if p.FallbackOnNotFound {
				continue
			}
			return nil, ErrNotFound
		default:
			resp.Body.Close()
			lastErr = fmt.Errorf("proxy %s: HTTP %d", p.URL, resp.StatusCode)
			// Non-404 server errors: only retry if comma-separated.
			if p.FallbackOnNotFound {
				continue
			}
			return nil, lastErr
		}
	}
	return nil, lastErr
}
