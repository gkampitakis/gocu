// Package cooldown parses durations that include day/week units.
package cooldown

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration accepts everything time.ParseDuration accepts plus `d` (days)
// and `w` (weeks). An empty string returns 0 (cooldown disabled). Compound
// values like "1w2d" are not supported — use a single unit.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	// Trailing `d` or `w` aren't recognized by time.ParseDuration. Strip and
	// convert manually.
	switch s[len(s)-1] {
	case 'd', 'w':
		unit := s[len(s)-1]
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		hours := 24.0
		if unit == 'w' {
			hours = 24 * 7
		}
		return time.Duration(n * hours * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (try 7d, 12h, 2w): %w", s, err)
	}
	return d, nil
}
