package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gkampitakis/gocu/internal/resolver"
)

func sampleUpgrades() []resolver.Result {
	return []resolver.Result{
		{Path: "ex.com/major", Current: "v1.0.0", Target: "v2.0.0", Bump: resolver.BumpMajor},
		{Path: "ex.com/minor", Current: "v1.0.0", Target: "v1.1.0", Bump: resolver.BumpMinor},
		{Path: "ex.com/patch", Current: "v1.0.0", Target: "v1.0.1", Bump: resolver.BumpPatch},
	}
}

func sendKey(m model, key string) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return next.(model)
}

func sendSpecial(m model, t tea.KeyType) model {
	next, _ := m.Update(tea.KeyMsg{Type: t})
	return next.(model)
}

func TestInitialModel_AllUnchecked(t *testing.T) {
	m := initialModel(sampleUpgrades())
	for i, c := range m.checked {
		if c {
			t.Errorf("checked[%d] should default to false", i)
		}
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestUpdate_CursorMovement(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m = sendKey(m, "j")
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}
	m = sendKey(m, "j")
	m = sendKey(m, "j") // already at bottom; clamp
	if m.cursor != 2 {
		t.Errorf("after 2 more j: cursor = %d, want 2 (clamped)", m.cursor)
	}
	m = sendKey(m, "k")
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}
	m = sendKey(m, "g")
	if m.cursor != 0 {
		t.Errorf("after g: cursor = %d, want 0", m.cursor)
	}
	m = sendKey(m, "G")
	if m.cursor != 2 {
		t.Errorf("after G: cursor = %d, want 2", m.cursor)
	}
}

func TestUpdate_ToggleCurrent(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m = sendKey(m, " ")
	if !m.checked[0] {
		t.Error("space should toggle cursor row on")
	}
	m = sendKey(m, " ")
	if m.checked[0] {
		t.Error("second space should toggle off")
	}
}

func TestUpdate_AllAndNone(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m = sendKey(m, "a")
	for i, c := range m.checked {
		if !c {
			t.Errorf("`a` should select all; checked[%d] = false", i)
		}
	}
	m = sendKey(m, "n")
	for i, c := range m.checked {
		if c {
			t.Errorf("`n` should deselect all; checked[%d] = true", i)
		}
	}
}

func TestUpdate_ToggleByBumpKind(t *testing.T) {
	m := initialModel(sampleUpgrades())

	// Toggle all majors on.
	m = sendKey(m, "M")
	if !m.checked[0] {
		t.Error("M should select the major row")
	}
	if m.checked[1] || m.checked[2] {
		t.Error("M should only affect majors")
	}

	// Pressing M again should clear it.
	m = sendKey(m, "M")
	if m.checked[0] {
		t.Error("second M should deselect the major row")
	}

	// Toggle minors and patches independently.
	m = sendKey(m, "m")
	m = sendKey(m, "p")
	if !m.checked[1] || !m.checked[2] {
		t.Error("m and p should select their respective rows")
	}
	if m.checked[0] {
		t.Error("m/p should not affect majors")
	}
}

func TestUpdate_EnterConfirms(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m = sendKey(m, "a")
	m = sendSpecial(m, tea.KeyEnter)
	if !m.confirmed {
		t.Error("enter should set confirmed=true")
	}
}

func TestUpdate_QuitSetsQuitFlag(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m = sendKey(m, "q")
	if !m.quit {
		t.Error("q should set quit=true")
	}
	if m.confirmed {
		t.Error("q should not set confirmed")
	}
}

func TestView_RendersWithoutError(t *testing.T) {
	// We can't easily inspect the rendered string semantically, but it should
	// at least produce non-empty output and mention each module path.
	m := initialModel(sampleUpgrades())
	out := m.View()
	if out == "" {
		t.Fatal("View returned empty string")
	}
	for _, u := range sampleUpgrades() {
		if !contains(out, u.Path) {
			t.Errorf("view missing module %s", u.Path)
		}
	}
}

func TestView_EmptyAfterQuit(t *testing.T) {
	m := initialModel(sampleUpgrades())
	m.quit = true
	if got := m.View(); got != "" {
		t.Errorf("quit view should be empty, got %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
