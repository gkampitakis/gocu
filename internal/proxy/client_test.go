package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Env{Proxies: []ProxyEntry{{URL: srv.URL, FallbackOnNotFound: true}}})
}

func TestClient_List(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/github.com/foo/bar/@v/list" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprintln(w, "v1.0.0\nv1.1.0\nv2.0.0")
	})

	versions, err := c.List(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.0.0", "v1.1.0", "v2.0.0"}
	if !equal(versions, want) {
		t.Errorf("List = %v, want %v", versions, want)
	}
}

func TestClient_ListCached(t *testing.T) {
	var hits int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		fmt.Fprintln(w, "v1.0.0")
	})

	for range 3 {
		if _, err := c.List(context.Background(), "github.com/foo/bar"); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (cache miss?)", got)
	}
}

func TestClient_Info(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/@v/v1.2.3.info") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"Version":"v1.2.3","Time":"2024-01-15T10:00:00Z"}`)
	})

	info, err := c.Info(context.Background(), "github.com/foo/bar", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.2.3" {
		t.Errorf("Version = %q, want v1.2.3", info.Version)
	}
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !info.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", info.Time, want)
	}
}

func TestClient_Latest(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/@latest") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"Version":"v2.0.0","Time":"2024-02-01T00:00:00Z"}`)
	})

	info, err := c.Latest(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v2.0.0" {
		t.Errorf("Version = %q, want v2.0.0", info.Version)
	}
}

func TestClient_NotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	})

	if _, err := c.List(context.Background(), "github.com/missing/mod"); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestClient_Fallback(t *testing.T) {
	var firstHits, secondHits int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstHits, 1)
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondHits, 1)
		fmt.Fprintln(w, "v1.0.0")
	}))
	defer second.Close()

	c := New(Env{Proxies: []ProxyEntry{
		{URL: first.URL, FallbackOnNotFound: true},
		{URL: second.URL, FallbackOnNotFound: true},
	}})

	versions, err := c.List(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Errorf("versions = %v, want one entry", versions)
	}
	if firstHits != 1 || secondHits != 1 {
		t.Errorf("hits = %d/%d, want 1/1", firstHits, secondHits)
	}
}

func TestClient_NoFallbackOnPipe(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer first.Close()
	var secondHit bool
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHit = true
		fmt.Fprintln(w, "v1.0.0")
	}))
	defer second.Close()

	// Pipe semantics for the second entry: don't fall through 404s.
	c := New(Env{Proxies: []ProxyEntry{
		{URL: first.URL, FallbackOnNotFound: false}, // first entry: fallback flag doesn't matter
		{URL: second.URL, FallbackOnNotFound: false},
	}})

	if _, err := c.List(context.Background(), "github.com/foo/bar"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if secondHit {
		t.Error("second proxy should not have been called under pipe semantics")
	}
}

func TestClient_Private(t *testing.T) {
	c := New(Env{
		Proxies: []ProxyEntry{{URL: "http://unused", FallbackOnNotFound: true}},
		Private: "github.com/secret/*",
	})
	if _, err := c.List(context.Background(), "github.com/secret/mod"); !errors.Is(
		err,
		ErrPrivate,
	) {
		t.Errorf("err = %v, want ErrPrivate", err)
	}
}

func TestParseGOPROXY(t *testing.T) {
	cases := []struct {
		in   string
		want []ProxyEntry
	}{
		{"", nil},
		{"https://a", []ProxyEntry{{"https://a", true}}},
		{"https://a,https://b", []ProxyEntry{{"https://a", true}, {"https://b", true}}},
		{"https://a|https://b", []ProxyEntry{{"https://a", true}, {"https://b", false}}},
		{
			"https://a,https://b|https://c",
			[]ProxyEntry{{"https://a", true}, {"https://b", true}, {"https://c", false}},
		},
		{"https://a,direct", []ProxyEntry{{"https://a", true}, {"direct", true}}},
	}
	for _, tc := range cases {
		got := parseGOPROXY(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parse(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parse(%q)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
