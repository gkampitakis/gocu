package filter

import "testing"

func TestCompile_Exact(t *testing.T) {
	m, err := Compile([]string{"github.com/foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match("github.com/foo/bar") {
		t.Error("expected exact match")
	}
	if m.Match("github.com/foo/baz") {
		t.Error("unexpected match")
	}
}

func TestCompile_Glob(t *testing.T) {
	m, _ := Compile([]string{"github.com/aws/*"})
	if !m.Match("github.com/aws/aws-sdk-go") {
		t.Error("expected glob match")
	}
	if m.Match("github.com/gcp/sdk") {
		t.Error("unexpected match")
	}
	if m.Match("github.com/aws/sdk/sub") {
		// `*` should not cross `/`
		t.Error("unexpected cross-slash match")
	}
}

func TestCompile_DoubleStar(t *testing.T) {
	m, _ := Compile([]string{"github.com/aws/**"})
	if !m.Match("github.com/aws/sdk/sub/pkg") {
		t.Error("expected ** to cross slashes")
	}
}

func TestCompile_Regex(t *testing.T) {
	m, _ := Compile([]string{"/internal/"})
	if !m.Match("github.com/foo/internal/bar") {
		t.Error("expected regex match")
	}
	if m.Match("github.com/foo/bar") {
		t.Error("unexpected match")
	}
}

func TestCompile_CommaSeparated(t *testing.T) {
	m, _ := Compile([]string{"a/b, c/d"})
	if !m.Match("a/b") || !m.Match("c/d") {
		t.Error("comma-separated patterns should both match")
	}
}

func TestCompile_InvalidRegex(t *testing.T) {
	if _, err := Compile([]string{"/[invalid/"}); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestEmpty(t *testing.T) {
	m, _ := Compile(nil)
	if !m.Empty() {
		t.Error("expected empty")
	}
	var nilM *Matcher
	if !nilM.Empty() {
		t.Error("nil matcher should be empty")
	}
	if nilM.Match("anything") {
		t.Error("nil matcher should not match")
	}
}
