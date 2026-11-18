package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

var (
	accentColor = lipgloss.Color("#38BDF8")
	borderColor = lipgloss.Color("#475569")
	dimColor    = lipgloss.Color("#94A3B8")
	selBgColor  = lipgloss.Color("#1D4ED8")
	favColor    = lipgloss.Color("#F59E0B")
	okColor     = lipgloss.Color("#22C55E")
	errColor    = lipgloss.Color("#EF4444")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(selBgColor).Bold(true)
	okStyle       = lipgloss.NewStyle().Foreground(okColor)
	errStyle      = lipgloss.NewStyle().Foreground(errColor)
	favStyle      = lipgloss.NewStyle().Foreground(favColor).Bold(true)
)

// levelColor is the dot color for a usefulness level.
func levelColor(level int) lipgloss.Color {
	switch level {
	case db.LevelEssential:
		return lipgloss.Color("#22C55E")
	case db.LevelCommon:
		return lipgloss.Color("#EAB308")
	case db.LevelAdvanced:
		return lipgloss.Color("#94A3B8")
	default:
		return dimColor
	}
}

// levelChip renders the active usefulness filter as a small badge.
func levelChip(level int) string {
	if level == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Background(levelColor(level)).
		Foreground(lipgloss.Color("#0F172A")).
		Bold(true).
		Render(" ● " + db.LevelName(level) + " ")
}

// View renders the whole screen.
func (m Model) View() string {
	if m.err != nil {
		return errStyle.Render("Error: "+m.err.Error()) + "\nPress Ctrl+C to quit."
	}
	if m.showHelp {
		return m.viewHelp()
	}
	var b strings.Builder
	b.WriteString(m.viewHeader() + "\n")
	b.WriteString(m.viewSearchBox() + "\n")
	b.WriteString(m.viewBody() + "\n")
	b.WriteString(m.viewFooter())
	return b.String()
}

func (m Model) viewHeader() string {
	left := titleStyle.Render("cheat-terminal") + dimStyle.Render(" v"+m.version)
	right := dimStyle.Render(fmt.Sprintf("%d entries", m.total))
	if chip := levelChip(m.level); chip != "" {
		right += "  " + chip
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) viewSearchBox() string {
	border := borderColor
	if m.focus == focusSearch {
		border = accentColor
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(max(10, m.width-2))
	return style.Render(m.input.View())
}

// layout returns sidebar, detail and list widths (totals, borders included).
func (m Model) layout() (sidebarW, detailW, listW int) {
	sidebarW = 24
	detailW = 0
	switch {
	case m.width >= 110:
		detailW = 42
	case m.width >= 90:
		detailW = 34
	}
	listW = m.width - sidebarW - detailW - 2 // 1-col gaps
	if listW < 20 {
		detailW = 0
		listW = m.width - sidebarW - 1
	}
	if listW < 20 {
		sidebarW = 20
		listW = m.width - sidebarW - 1
	}
	if listW < 10 {
		listW = 10
	}
	return sidebarW, detailW, listW
}

func (m Model) viewBody() string {
	sidebarW, detailW, listW := m.layout()
	bodyH := m.bodyHeight()
	sidebar := m.viewSidebar(sidebarW-2, bodyH-2)
	list := m.viewList(listW, bodyH)
	if detailW == 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", list)
	}
	detail := m.viewDetail(detailW-2, bodyH-2)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", list, " ", detail)
}

func (m Model) viewSidebar(innerW, innerH int) string {
	var lines []string
	lines = append(lines, titleStyle.Render("Categories"))
	lines = append(lines, dimStyle.Render(strings.Repeat("─", max(1, innerW))))
	for i, item := range m.sides {
		// The row is built one cell narrower than the box: non-selected
		// rows get a leading space, and the selected row is padded by its
		// style. A row exactly as wide as the box would wrap.
		w := innerW - 1
		suffix := fmt.Sprintf("(%d)", item.count)
		label := truncate(item.title, max(1, w-len(suffix)-1))
		gap := w - lipgloss.Width(label) - len(suffix)
		line := label + strings.Repeat(" ", max(0, gap)) + suffix
		if i == m.activeSide {
			lines = append(lines, selectedStyle.Width(innerW).Render(line))
		} else {
			lines = append(lines, " "+line)
		}
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)
	return style.Render(strings.Join(lines[:innerH], "\n"))
}

func (m Model) viewList(listW, bodyH int) string {
	var lines []string
	if len(m.results) == 0 {
		lines = append(lines, dimStyle.Render("No matches."))
	} else {
		end := m.offset + bodyH
		if end > len(m.results) {
			end = len(m.results)
		}
		for i := m.offset; i < end; i++ {
			lines = append(lines, m.viewRow(m.results[i], i == m.cursor, listW))
		}
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return strings.Join(lines[:bodyH], "\n")
}

func (m Model) viewRow(e db.Entry, selected bool, listW int) string {
	star := "  "
	if e.Favorite {
		star = favStyle.Render("★ ")
	}
	dot := lipgloss.NewStyle().Foreground(levelColor(e.Level)).Render("●")
	text := e.Command
	if strings.TrimSpace(e.Description) != "" {
		text += "  —  " + e.Description
	}
	text = truncate(text, max(1, listW-5))
	line := star + dot + " " + text
	if selected {
		return selectedStyle.Width(listW).Render(line)
	}
	return " " + line
}

// viewDetail renders the detail pane for the selected entry.
func (m Model) viewDetail(innerW, innerH int) string {
	var lines []string
	e, ok := m.selected()
	if !ok {
		lines = append(lines, titleStyle.Render("Details"))
		lines = append(lines, dimStyle.Render("Nothing selected."))
	} else {
		lines = append(lines, titleStyle.Render("Details"))
		lines = append(lines, dimStyle.Render(strings.Repeat("─", max(1, innerW))))
		lines = append(lines, dimStyle.Render("Command:"))
		for _, ln := range wrapText(e.Command, innerW) {
			lines = append(lines, titleStyle.Render(ln))
		}
		if strings.TrimSpace(e.Description) != "" {
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("Description:"))
			lines = append(lines, wrapText(e.Description, innerW)...)
		}
		if strings.TrimSpace(e.Example) != "" {
			lines = append(lines, "")
			lines = append(lines, dimStyle.Render("Example:"))
			lines = append(lines, wrapText(e.Example, innerW)...)
		}
		if strings.TrimSpace(e.Tags) != "" {
			lines = append(lines, "")
			for _, ln := range wrapText("Tags: "+e.Tags, innerW) {
				lines = append(lines, dimStyle.Render(ln))
			}
		}
		lines = append(lines, "")
		levelLine := "Level: " + lipgloss.NewStyle().Foreground(levelColor(e.Level)).Render("●") +
			" " + db.LevelName(e.Level)
		lines = append(lines, levelLine)
		meta := fmt.Sprintf("%s · #%d · %s", e.CategoryTitle, e.ID, e.Source)
		if e.Favorite {
			meta += "  ★ Favorite"
		}
		lines = append(lines, dimStyle.Render(truncate(meta, innerW)))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(truncate("[Enter] Copy command   [s] Favorite", innerW)))
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)
	return style.Render(strings.Join(lines[:innerH], "\n"))
}

func (m Model) viewFooter() string {
	var help string
	if m.focus == focusList {
		help = "↑↓/j/k move · Tab category · Enter/c copy · s favorite · f level filter · / search · ? help · q quit"
	} else {
		help = "Type to search · ↑↓ move · Tab category · Enter copy · Esc list mode · ? help · Ctrl+C quit"
	}
	footer := dimStyle.Render(truncate(help, max(10, m.width-1))) + "\n"
	if m.status != "" {
		if m.statusErr {
			footer += errStyle.Render(truncate(m.status, max(10, m.width-1)))
		} else {
			footer += okStyle.Render(truncate(m.status, max(10, m.width-1)))
		}
		return footer
	}
	count := fmt.Sprintf("%d entries", len(m.results))
	if m.activeSide >= 0 && m.activeSide < len(m.sides) {
		count += " · " + m.sides[m.activeSide].title
	}
	if m.level != 0 {
		count += " · " + db.LevelName(m.level)
	}
	footer += dimStyle.Render(count)
	return footer
}

func (m Model) viewHelp() string {
	lines := []string{
		titleStyle.Render("cheat-terminal — Keyboard shortcuts"),
		"",
		titleStyle.Render("SEARCH MODE (default)"),
		"  Type                 Filter commands live",
		"  ↑ / ↓                Move selection",
		"  PgUp / PgDn          Jump a page",
		"  Tab / Shift+Tab      Switch category",
		"  Enter                Copy selected command",
		"  Esc                  Clear search, then switch to list mode",
		"  ?                    This help",
		"  Ctrl+C               Quit",
		"",
		titleStyle.Render("LIST MODE (Esc from search)"),
		"  ↑ / ↓ , j / k        Move selection",
		"  Enter / c            Copy selected command",
		"  s                    Star / unstar favorite",
		"  f                    Filter by usefulness: All → Essential → Common → Advanced",
		"  /                    Back to search",
		"  q                    Quit",
		"",
		dimStyle.Render("Esc / ? closes this help."),
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// runeLen counts display cells (runes), unlike len() which counts bytes.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
