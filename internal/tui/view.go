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
	darkText    = lipgloss.Color("#0F172A")

	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	headerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	dimStyle         = lipgloss.NewStyle().Foreground(dimColor)
	keyStyle         = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	selectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(selBgColor).Bold(true)
	selectedDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#DBEAFE")).Background(selBgColor)
	okStyle          = lipgloss.NewStyle().Foreground(okColor)
	errStyle         = lipgloss.NewStyle().Foreground(errColor)
	favStyle         = lipgloss.NewStyle().Foreground(favColor).Bold(true)
)

// categoryPalette colors user-created categories: each name hashes to
// one of these, so a category keeps its color across runs.
var categoryPalette = []lipgloss.Color{
	"#34D399", "#F472B6", "#FACC15", "#2DD4BF",
	"#FB7185", "#A3E635", "#38BDF8", "#C084FC",
}

// categoryColor is the color of a category: the four built-in ones have
// fixed colors, Favorites is gold, and user categories take a color
// from the palette by hashing their name.
func categoryColor(name string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "linux":
		return lipgloss.Color("#60A5FA")
	case "docker":
		return lipgloss.Color("#22D3EE")
	case "git":
		return lipgloss.Color("#FB923C")
	case "powershell":
		return lipgloss.Color("#A78BFA")
	case "favorites", "★ favorites":
		return favColor
	case "all":
		return accentColor
	}
	h := 0
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return categoryPalette[h%len(categoryPalette)]
}

// chip renders a small colored badge: dark bold text on a colored
// background, e.g. a category or a tag.
func chip(label string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(darkText).
		Bold(true).
		Render(" " + label + " ")
}

// categoryChip renders a category name as a colored badge.
func categoryChip(name, title string) string {
	return chip(title, categoryColor(name))
}

// tagChips renders tags as colored badges, wrapped to the given width.
// Each tag takes the next color from the palette.
func tagChips(tags string, width int) []string {
	var names []string
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			names = append(names, t)
		}
	}
	if len(names) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for i, name := range names {
		c := chip(name, categoryPalette[i%len(categoryPalette)])
		if cur == "" {
			cur = c
			continue
		}
		if lipgloss.Width(cur)+1+lipgloss.Width(c) > width {
			lines = append(lines, cur)
			cur = c
		} else {
			cur += " " + c
		}
	}
	return append(lines, cur)
}

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
		Foreground(darkText).
		Bold(true).
		Render(" ● " + db.LevelName(level) + " ")
}

// View renders the whole screen.
func (m Model) View() string {
	if m.err != nil {
		return errStyle.Render("Error: "+m.err.Error()) + "\nPress Ctrl+C to quit."
	}
	if m.prompt != promptNone {
		return m.viewPrompt()
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
	left := headerTitleStyle.Render("cheat-terminal") + dimStyle.Render(" v"+m.version)
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
		label := truncate(item.title, max(1, w-len(suffix)-3))
		gap := w - 2 - lipgloss.Width(label) - len(suffix)
		// Every row carries its category color: a colored dot, and a
		// colored label when the row is not selected. The selected row
		// keeps white text on the accent background so it stays readable.
		color := categoryColor(item.name)
		if item.kind == sideFavorites {
			color = favColor
		} else if item.kind == sideAll {
			color = accentColor
		}
		dot := lipgloss.NewStyle().Foreground(color).Render("●")
		if i == m.activeSide {
			line := dot + " " + label + strings.Repeat(" ", max(0, gap)) + suffix
			lines = append(lines, selectedStyle.Width(innerW).Render(line))
		} else {
			colored := lipgloss.NewStyle().Foreground(color).Render(label)
			line := dot + " " + colored + strings.Repeat(" ", max(0, gap)) + dimStyle.Render(suffix)
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
		end := m.offset + m.visibleRows()
		if end > len(m.results) {
			end = len(m.results)
		}
		for i := m.offset; i < end; i++ {
			lines = append(lines, m.viewRowLines(m.results[i], i == m.cursor, listW)...)
		}
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return strings.Join(lines[:bodyH], "\n")
}

// viewRowLines renders one entry as two lines plus a blank separator:
// line 1 is the level dot, the bold command and a star for favorites;
// line 2 is the dim description, indented under the command.
func (m Model) viewRowLines(e db.Entry, selected bool, listW int) []string {
	star := ""
	if e.Favorite {
		star = " " + favStyle.Render("★")
	}
	// Truncate the command first (plain text), then style it: cutting a
	// styled string would break its escape codes.
	cmd := truncate(e.Command, max(1, listW-7))
	dot := lipgloss.NewStyle().Foreground(levelColor(e.Level)).Render("●")
	line1 := dot + " " + titleStyle.Render(cmd) + star
	if selected {
		line1 = selectedStyle.Width(listW).Render(line1)
	} else {
		line1 = " " + line1
	}
	desc := truncate(strings.TrimSpace(e.Description), max(1, listW-4))
	if selected {
		// No inner dim style here: the outer style colors the whole
		// line light-on-blue so the row reads as one block.
		return []string{line1, selectedDimStyle.Width(listW).Render("   " + desc), ""}
	}
	return []string{line1, "   " + dimStyle.Render(desc), ""}
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
		// Category and level as colored chips; the star marks a favorite.
		chips := categoryChip(e.Category, e.CategoryTitle) + " " + levelChip(e.Level)
		if e.Favorite {
			chips += "  " + favStyle.Render("★ Favorite")
		}
		if lipgloss.Width(chips) > innerW {
			lines = append(lines, categoryChip(e.Category, e.CategoryTitle))
			lines = append(lines, levelChip(e.Level))
		} else {
			lines = append(lines, chips)
		}
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
			lines = append(lines, dimStyle.Render("Tags:"))
			lines = append(lines, tagChips(e.Tags, innerW)...)
		}
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("#%d · %s", e.ID, e.Source)))
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(truncate("[Enter] Copy   [s] Favorite   [a] Add   [d] Delete", innerW)))
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

// keyHint is one footer hint: an accent key name plus a dim description.
type keyHint struct {
	key  string
	desc string
}

// footerHints is the main key list. In list mode every key works; in
// search mode Esc switches to list mode, where they all become available.
func footerHints(listMode bool) []keyHint {
	if listMode {
		return []keyHint{
			{"Enter", "copy"}, {"/", "search"}, {"f", "filter"},
			{"s", "favorite"}, {"a", "add"}, {"C", "category"},
			{"d", "delete"}, {"?", "help"}, {"q", "quit"},
		}
	}
	return []keyHint{
		{"Enter", "copy"}, {"Esc", "list"}, {"f", "filter"},
		{"s", "favorite"}, {"a", "add"}, {"C", "category"},
		{"d", "delete"}, {"?", "help"}, {"Ctrl+C", "quit"},
	}
}

// footerHintsShort is the compact key list for narrow terminals.
func footerHintsShort(listMode bool) []keyHint {
	if listMode {
		return []keyHint{
			{"⏎", "copy"}, {"/", "search"}, {"f", "filter"},
			{"s", "fav"}, {"a", "add"}, {"C", "cat"},
			{"d", "del"}, {"?", "help"}, {"q", "quit"},
		}
	}
	return []keyHint{
		{"⏎", "copy"}, {"Esc", "list"}, {"f", "filter"},
		{"s", "fav"}, {"a", "add"}, {"C", "cat"},
		{"d", "del"}, {"?", "help"},
	}
}

// renderKeyHints joins the hints as "key desc · key desc ...", with the
// key names in accent bold and the descriptions dim. It returns the
// rendered line and its plain-text width.
func renderKeyHints(hints []keyHint) (string, int) {
	var plain strings.Builder
	var styled strings.Builder
	for i, h := range hints {
		if i > 0 {
			plain.WriteString(" · ")
			styled.WriteString(dimStyle.Render(" · "))
		}
		plain.WriteString(h.key + " " + h.desc)
		styled.WriteString(keyStyle.Render(h.key) + " " + dimStyle.Render(h.desc))
	}
	return styled.String(), runeLen(plain.String())
}

func (m Model) viewFooter() string {
	avail := max(10, m.width-1)
	hints := footerHints(m.focus == focusList)
	rendered, width := renderKeyHints(hints)
	if width > avail {
		rendered, width = renderKeyHints(footerHintsShort(m.focus == focusList))
	}
	footer := rendered
	if width > avail {
		// Very narrow terminal: fall back to a plain truncated line.
		footer = dimStyle.Render(truncate("Enter copy · Esc list · ? help · q quit", avail))
	}
	footer += "\n"
	if m.status != "" {
		if m.statusErr {
			footer += errStyle.Render(truncate(m.status, avail))
		} else {
			footer += okStyle.Render(truncate(m.status, avail))
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
	// Kept short on purpose: the box (border + padding included) must
	// fit in a 24-row terminal, or its top lines scroll off.
	lines := []string{
		headerTitleStyle.Render("cheat-terminal — Keyboard shortcuts"),
		"",
		titleStyle.Render("SEARCH — type to filter"),
		"  ↑↓ move · Tab category · Enter copy",
		"  Esc clear, then list mode · ? help · Ctrl+C quit",
		"",
		titleStyle.Render("LIST (Esc from search)"),
		"  ↑↓ j k move · Enter / c copy · s favorite · f filter",
		"  a Add a new entry · C Add a new category",
		"  d Delete the selected entry · / search · q quit",
		"",
		titleStyle.Render("ADD ENTRY (a) — Enter next step, Esc cancel"),
		"  Command → Description → Category → Tags → Level",
		"  Category: existing name, or a new name creates it",
		"  Level: 1 Essential, 2 Common, 3 Advanced (empty = Common)",
		"",
		titleStyle.Render("DELETE (d) — y deletes, n / Esc keeps it"),
		"",
		dimStyle.Render("Esc / ? closes this help."),
	}
	// Safety net for very short terminals: never render taller than the
	// screen, even if a future edit adds lines.
	if maxLines := m.height - 4; maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewPrompt renders the open prompt overlay: the add-entry form, the
// add-category prompt, or the delete confirmation.
func (m Model) viewPrompt() string {
	switch m.prompt {
	case promptAdd:
		return m.viewAddForm()
	case promptCategory:
		return m.viewCategoryForm()
	case promptDelete:
		return m.viewDeleteConfirm()
	}
	return ""
}

// promptBox centers a bordered box of the given lines on the screen.
func (m Model) promptBox(lines []string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// promptWidth is the content width of the prompt boxes, sized to the
// terminal.
func (m Model) promptWidth() int {
	w := m.width - 10
	if w > 60 {
		w = 60
	}
	if w < 30 {
		w = 30
	}
	return w
}

func (m Model) viewAddForm() string {
	w := m.promptWidth()
	step := m.addStep + 1
	lines := []string{
		headerTitleStyle.Render(fmt.Sprintf("Add entry — step %d of %d: %s", step, addSteps, addStepLabels[m.addStep])),
		"",
		dimStyle.Render(truncate(addStepHints[m.addStep], w)),
	}
	if m.addStep == 2 {
		// Step 3 lists the existing categories, so the user can pick one
		// or see that a new name will create a category.
		lines = append(lines, dimStyle.Render(truncate("Existing: "+m.categoryTitles(), w)))
	}
	// What has been filled in so far, so the user sees where they are.
	done := []string{m.addCmd, m.addDesc, m.addCat, m.addTags}
	for i := 0; i < m.addStep; i++ {
		if done[i] == "" {
			continue
		}
		lines = append(lines, dimStyle.Render(truncate(addStepLabels[i]+": "+done[i], w)))
	}
	lines = append(lines, "")
	lines = append(lines, m.formInput.View())
	if m.addErr != "" {
		lines = append(lines, errStyle.Render(truncate(m.addErr, w)))
	}
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("Enter next step · Esc cancel"))
	return m.promptBox(lines)
}

// categoryTitles lists the existing category titles for the add-form hint.
func (m Model) categoryTitles() string {
	var titles []string
	for _, s := range m.sides {
		if s.kind == sideCategory {
			titles = append(titles, s.title)
		}
	}
	return strings.Join(titles, ", ")
}

func (m Model) viewCategoryForm() string {
	w := m.promptWidth()
	lines := []string{
		headerTitleStyle.Render("Add category"),
		"",
		dimStyle.Render(truncate("A new category appears in the sidebar.", w)),
		dimStyle.Render(truncate("Existing: "+m.categoryTitles(), w)),
		"",
		m.formInput.View(),
		"",
		dimStyle.Render("Enter save · Esc cancel"),
	}
	return m.promptBox(lines)
}

func (m Model) viewDeleteConfirm() string {
	e, ok := m.selected()
	lines := []string{
		headerTitleStyle.Render("Delete this entry?"),
		"",
	}
	if ok {
		lines = append(lines, titleStyle.Render(truncate(e.Command, m.promptWidth())))
		if strings.TrimSpace(e.Description) != "" {
			lines = append(lines, dimStyle.Render(truncate(e.Description, m.promptWidth())))
		}
		lines = append(lines, "")
	}
	lines = append(lines, dimStyle.Render("Press y to delete, n or Esc to keep it."))
	return m.promptBox(lines)
}

// runeLen counts display cells (runes), unlike len() which counts bytes.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
