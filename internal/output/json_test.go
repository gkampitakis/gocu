package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/gkampitakis/gocu/internal/resolver"
)

func TestWriteJSON_Schema(t *testing.T) {
	var buf bytes.Buffer
	pub := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	err := WriteJSON(&buf, []resolver.Result{
		{
			Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0",
			Bump:        resolver.BumpMinor,
			PublishedAt: pub,
		},
		{Path: "ex.com/pinned", Current: "v1.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []JSONUpgrade
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Module != "ex.com/a" || got[0].Target != "v1.1.0" ||
		got[0].Bump != "minor" || !got[0].PublishedAt.Equal(pub) {
		t.Errorf("upgrade entry mismatch: %+v", got[0])
	}
	// Pinned entries are included with no Target/Bump.
	if got[1].Target != "" || got[1].Bump != "" {
		t.Errorf("pinned entry should have empty Target/Bump: %+v", got[1])
	}
}

func TestWriteJSON_OmitsZeroPublishedAt(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, []resolver.Result{
		{Path: "ex.com/a", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A zero time.Time must not appear as the "0001-01-01" sentinel; the
	// `omitzero` JSON tag should drop the field entirely.
	if got := buf.String(); contains(got, "0001-01-01") || contains(got, "published_at") {
		t.Errorf("zero PublishedAt should be omitted: %s", got)
	}
}

func TestWriteJSON_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	// json.Encoder appends a newline; result should still be valid JSON.
	var out []JSONUpgrade
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON for empty input: %v\n%s", err, buf.String())
	}
	if len(out) != 0 {
		t.Errorf("expected empty array, got %v", out)
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
