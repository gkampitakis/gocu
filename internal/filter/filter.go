// Package filter compiles ncu-style pattern lists (glob, regex, exact) into a
// single matcher.
package filter

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Matcher reports whether a name matches any compiled pattern.
type Matcher struct {
	exact   map[string]struct{}
	globs   []string
	regexes []*regexp.Regexp
}

// Compile builds a Matcher from a list of pattern strings. Patterns are:
//   - `/regex/` — Go regexp
//   - glob (containing `*` or `?`) — uses path.Match semantics
//   - anything else — exact match
//
// An empty list returns a Matcher that never matches.
func Compile(patterns []string) (*Matcher, error) {
	m := &Matcher{exact: map[string]struct{}{}}
	for _, p := range patterns {
		// Allow comma-separated entries within a single string for config-file
		// ergonomics: `--filter a,b` and `--filter a --filter b` both work.
		for sub := range strings.SplitSeq(p, ",") {
			sub = strings.TrimSpace(sub)
			if sub == "" {
				continue
			}
			if err := m.add(sub); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

func (m *Matcher) add(p string) error {
	switch {
	case len(p) >= 2 && p[0] == '/' && p[len(p)-1] == '/':
		re, err := regexp.Compile(p[1 : len(p)-1])
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", p, err)
		}
		m.regexes = append(m.regexes, re)
	case strings.ContainsAny(p, "*?"):
		m.globs = append(m.globs, p)
	default:
		m.exact[p] = struct{}{}
	}
	return nil
}

// Empty reports whether the matcher has no patterns at all.
func (m *Matcher) Empty() bool {
	return m == nil || (len(m.exact) == 0 && len(m.globs) == 0 && len(m.regexes) == 0)
}

// Match reports whether name matches any compiled pattern.
func (m *Matcher) Match(name string) bool {
	if m == nil {
		return false
	}
	if _, ok := m.exact[name]; ok {
		return true
	}
	for _, g := range m.globs {
		if matchGlob(g, name) {
			return true
		}
	}
	for _, re := range m.regexes {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// matchGlob is path.Match with a tweak: `*` should match path separators in
// module paths (`github.com/aws/*` is intuitive ncu-style). We implement that
// by treating the pattern as a sequence of literal-vs-wildcard segments.
func matchGlob(pattern, name string) bool {
	// path.Match doesn't cross `/` boundaries, so do it segment-aware: split on
	// `/` only if both pattern and name have the same number of segments,
	// otherwise fall back to a "**"-style match.
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, name)
		return ok
	}
	// Convert glob to regex: `*` -> `[^/]*`, `**` -> `.*`, `?` -> `[^/]`.
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}
