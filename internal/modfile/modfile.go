// Package modfile reads and inspects a project's go.mod file.
package modfile

import (
	"fmt"
	"os"

	"golang.org/x/mod/modfile"
)

// Module is a single require entry in a go.mod file.
type Module struct {
	Path     string
	Version  string
	Indirect bool
	Replaced bool
}

// File wraps a parsed go.mod plus the source path it came from.
type File struct {
	Path string
	mod  *modfile.File
}

// Parse reads and parses the go.mod at path.
func Parse(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	mf, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &File{Path: path, mod: mf}, nil
}

// ModulePath returns the module path declared at the top of go.mod.
func (f *File) ModulePath() string {
	if f.mod.Module == nil {
		return ""
	}
	return f.mod.Module.Mod.Path
}

// GoVersion returns the go directive (e.g., "1.22").
func (f *File) GoVersion() string {
	if f.mod.Go == nil {
		return ""
	}
	return f.mod.Go.Version
}

// Modules returns every require entry, marking ones replaced via a `replace`
// directive so callers can skip them.
func (f *File) Modules() []Module {
	replaced := map[string]bool{}
	for _, r := range f.mod.Replace {
		replaced[r.Old.Path] = true
	}

	out := make([]Module, 0, len(f.mod.Require))
	for _, r := range f.mod.Require {
		out = append(out, Module{
			Path:     r.Mod.Path,
			Version:  r.Mod.Version,
			Indirect: r.Indirect,
			Replaced: replaced[r.Mod.Path],
		})
	}
	return out
}
