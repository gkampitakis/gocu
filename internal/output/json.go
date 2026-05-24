package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/gkampitakis/gocu/internal/resolver"
)

// JSONUpgrade is the per-module record in JSON output.
type JSONUpgrade struct {
	Module      string    `json:"module"`
	Current     string    `json:"current"`
	Target      string    `json:"target,omitempty"`
	Bump        string    `json:"bump,omitempty"`
	PublishedAt time.Time `json:"published_at,omitzero"`
}

// WriteJSON emits all results (upgrades and pinned/up-to-date entries) as a
// stable JSON array.
func WriteJSON(w io.Writer, results []resolver.Result) error {
	out := make([]JSONUpgrade, 0, len(results))
	for _, r := range results {
		entry := JSONUpgrade{
			Module:      r.Path,
			Current:     r.Current,
			Target:      r.Target,
			PublishedAt: r.PublishedAt,
		}
		if r.Target != "" {
			entry.Bump = r.Bump.String()
		}
		out = append(out, entry)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
