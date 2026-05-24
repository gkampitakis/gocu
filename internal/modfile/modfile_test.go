package modfile

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleGoMod = `module example.com/foo

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.5.0 // indirect
)

require github.com/single/dep v2.0.0+incompatible

replace github.com/foo/bar => ../local/bar
`

func TestParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(sampleGoMod), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got, want := f.ModulePath(), "example.com/foo"; got != want {
		t.Errorf("ModulePath = %q, want %q", got, want)
	}
	if got, want := f.GoVersion(), "1.22"; got != want {
		t.Errorf("GoVersion = %q, want %q", got, want)
	}

	mods := f.Modules()
	if len(mods) != 3 {
		t.Fatalf("Modules len = %d, want 3", len(mods))
	}

	want := map[string]Module{
		"github.com/foo/bar":    {Path: "github.com/foo/bar", Version: "v1.2.3", Replaced: true},
		"github.com/baz/qux":    {Path: "github.com/baz/qux", Version: "v0.5.0", Indirect: true},
		"github.com/single/dep": {Path: "github.com/single/dep", Version: "v2.0.0+incompatible"},
	}
	for _, m := range mods {
		w, ok := want[m.Path]
		if !ok {
			t.Errorf("unexpected module %q", m.Path)
			continue
		}
		if m != w {
			t.Errorf("module %s = %+v, want %+v", m.Path, m, w)
		}
	}
}

func TestParse_missingFile(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected error for missing file")
	}
}
