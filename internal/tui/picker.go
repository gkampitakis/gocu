// Package tui hosts the interactive upgrade picker.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gkampitakis/gocu/internal/output"
	"github.com/gkampitakis/gocu/internal/resolver"
)

// Pick runs the interactive picker. It returns the selected upgrades plus a
// flag indicating whether the user confirmed (true) or quit (false).
func Pick(results []resolver.Result) ([]resolver.Result, bool, error) {
	upgrades := make([]resolver.Result, 0, len(results))
	for _, r := range results {
		if r.Target != "" {
			upgrades = append(upgrades, r)
		}
	}
	if len(upgrades) == 0 {
		return nil, false, nil
	}
	sort.Slice(upgrades, func(i, j int) bool {
		if upgrades[i].Bump != upgrades[j].Bump {
			return upgrades[i].Bump > upgrades[j].Bump
		}
		return upgrades[i].Path < upgrades[j].Path
	})

	m := initialModel(upgrades)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	out, err := prog.Run()
	if err != nil {
		return nil, false, err
	}
	final := out.(model)
	if !final.confirmed {
		return nil, false, nil
	}
	selected := make([]resolver.Result, 0, len(upgrades))
	for i, u := range final.upgrades {
		if final.checked[i] {
			selected = append(selected, u)
		}
	}
	return selected, true, nil
}

type model struct {
	upgrades  []resolver.Result
	checked   []bool
	cursor    int
	confirmed bool
	quit      bool
	width     int
}

func initialModel(upgrades []resolver.Result) model {
	return model{upgrades: upgrades, checked: make([]bool, len(upgrades))}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.upgrades)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(m.upgrades) - 1
		case " ", "x":
			m.checked[m.cursor] = !m.checked[m.cursor]
		case "a":
			for i := range m.checked {
				m.checked[i] = true
			}
		case "n":
			for i := range m.checked {
				m.checked[i] = false
			}
		case "M":
			m.toggleByBump(resolver.BumpMajor)
		case "m":
			m.toggleByBump(resolver.BumpMinor)
		case "p":
			m.toggleByBump(resolver.BumpPatch)
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) toggleByBump(kind resolver.BumpKind) {
	// If any of this kind is checked, uncheck them all; otherwise check them all.
	anyChecked := false
	for i, u := range m.upgrades {
		if u.Bump == kind && m.checked[i] {
			anyChecked = true
			break
		}
	}
	for i, u := range m.upgrades {
		if u.Bump == kind {
			m.checked[i] = !anyChecked
		}
	}
}

var (
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Underline(true)
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	majorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	minorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	patchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func (m model) View() string {
	if m.quit {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Select upgrades to apply"))
	b.WriteString("\n\n")

	// Column widths so paths align.
	maxPath, maxCur := 0, 0
	for _, u := range m.upgrades {
		if len(u.Path) > maxPath {
			maxPath = len(u.Path)
		}
		if len(u.Current) > maxCur {
			maxCur = len(u.Current)
		}
	}

	for i, u := range m.upgrades {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▶ ")
		}
		check := "[ ]"
		if m.checked[i] {
			check = "[x]"
		}
		// Pad based on the plain-text path length so OSC 8 escapes don't
		// throw off column alignment.
		pathLink := output.Hyperlink(output.PkgDevURL(u.Path), u.Path)
		pathPad := strings.Repeat(" ", maxPath-len(u.Path))
		curPad := strings.Repeat(" ", maxCur-len(u.Current))
		target := styleFor(u.Bump).Render(u.Target)
		bump := dimStyle.Render(fmt.Sprintf("(%s)", u.Bump))
		line := fmt.Sprintf(
			"%s%s  %s%s  %s%s → %s  %s",
			cursor, check,
			pathLink, pathPad,
			u.Current, curPad,
			target, bump,
		)
		b.WriteString(line)
		b.WriteString("\n")
	}

	selected := 0
	for _, c := range m.checked {
		if c {
			selected++
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render(fmt.Sprintf("%d/%d selected", selected, len(m.upgrades))))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(
		"↑/↓ move • space toggle • a all • n none • M/m/p toggle major/minor/patch • enter apply • q quit",
	))
	return b.String()
}

func styleFor(b resolver.BumpKind) lipgloss.Style {
	switch b {
	case resolver.BumpMajor:
		return majorStyle
	case resolver.BumpMinor:
		return minorStyle
	case resolver.BumpPatch:
		return patchStyle
	}
	return dimStyle
}
