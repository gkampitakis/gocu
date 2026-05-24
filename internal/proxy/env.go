package proxy

import (
	"os"
	"strings"

	"golang.org/x/mod/module"
)

// Env captures the subset of go-env variables we care about.
type Env struct {
	// Proxies is the ordered list parsed from GOPROXY. Each entry is either a
	// URL, "direct", or "off". The separator between adjacent entries
	// determines fallback behavior: "," means try the next on any error
	// (including 404/410), "|" means try the next only on transport errors.
	Proxies []ProxyEntry
	// Private is the comma-separated GOPRIVATE/GONOPROXY glob list. Modules
	// matching it should NOT be sent to the proxy.
	Private string
}

// ProxyEntry is one element of GOPROXY with the separator that preceded it.
type ProxyEntry struct {
	URL string // URL, "direct", or "off"
	// FallbackOnNotFound: true if a 404/410 from this entry should trigger
	// trying the next entry (comma separator). False for pipe.
	FallbackOnNotFound bool
}

// LoadEnv reads proxy/private configuration from the OS environment with
// Go-toolchain defaults.
func LoadEnv() Env {
	return Env{
		Proxies: parseGOPROXY(getenv("GOPROXY", "https://proxy.golang.org,direct")),
		Private: joinNonEmpty(",", getenv("GOPRIVATE", ""), getenv("GONOPROXY", "")),
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func getenv(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return def
}

// parseGOPROXY walks the GOPROXY string, splitting on ',' and '|' while
// remembering which separator preceded each entry.
func parseGOPROXY(raw string) []ProxyEntry {
	if raw == "" {
		return nil
	}
	var out []ProxyEntry
	fallback := true // first entry: error handling moot, default to comma semantics
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ',' || raw[i] == '|' {
			url := strings.TrimSpace(raw[start:i])
			if url != "" {
				out = append(out, ProxyEntry{URL: url, FallbackOnNotFound: fallback})
			}
			if i < len(raw) {
				fallback = raw[i] == ','
			}
			start = i + 1
		}
	}
	return out
}

// IsPrivate reports whether modulePath matches any GOPRIVATE/GONOPROXY pattern.
func (e Env) IsPrivate(modulePath string) bool {
	if e.Private == "" {
		return false
	}
	return module.MatchPrefixPatterns(e.Private, modulePath)
}
