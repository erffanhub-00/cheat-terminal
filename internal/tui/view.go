package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/erffanhub-00/cheat-terminal/internal/db"
)

// All colors are plain ANSI palette numbers ("0"-"255"), never hex or
// truecolor: the browser must look right on any terminal, from Windows
// Terminal to a bare xterm. The standard 16 are used where the look
// allows; git orange has no match there, so it takes 208.
var (
	accentColor = lipgloss.Color("12") // bright blue
	borderColor = lipgloss.Color("8")  // gray
	dimColor    = lipgloss.Color("7")  // light gray
	favColor    = lipgloss.Color("11") // bright yellow
	okColor     = lipgloss.Color("10") // bright green
	errColor    = lipgloss.Color("9")  // bright red

	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	headerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	dimStyle         = lipgloss.NewStyle().Foreground(dimColor)
	keyStyle         = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	okStyle          = lipgloss.NewStyle().Foreground(okColor)
	errStyle         = lipgloss.NewStyle().Foreground(errColor)
	favStyle         = lipgloss.NewStyle().Foreground(favColor).Bold(true)
	arrowStyle       = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	selectedCmdStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
)

// categoryPalette colors user-created categories: each name hashes to
// one of these, so a category keeps its color across runs.
var categoryPalette = []lipgloss.Color{
	"2", "13", "11", "14",
	"9", "10", "12", "5",
}

// categoryColor is the color of a category: the four built-in ones have
// fixed colors, Favorites is gold, and user categories take a color
// from the palette by hashing their name.
func categoryColor(name string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "linux":
		return lipgloss.Color("12")
	case "docker":
		return lipgloss.Color("14")
	case "git":
		return lipgloss.Color("208")
	case "powershell":
		return lipgloss.Color("13")
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

// brightColor returns the bright variant of an ANSI palette color, so
// text stays readable on a dark terminal: dark colors (0-7) become
// their bright counterparts, bright ones stay as they are. Black text
// on a dark chip background was the unreadable part; bright text with
// no background fill has no foreground/background pair to get wrong.
func brightColor(c lipgloss.Color) lipgloss.Color {
	switch string(c) {
	case "0":
		return lipgloss.Color("15")
	case "1":
		return lipgloss.Color("9")
	case "2":
		return lipgloss.Color("10")
	case "3":
		return lipgloss.Color("11")
	case "4":
		return lipgloss.Color("12")
	case "5":
		return lipgloss.Color("13")
	case "6":
		return lipgloss.Color("14")
	case "8":
		return lipgloss.Color("7")
	}
	return c
}

// bracketChip renders a label as bright bold colored text in brackets,
// with no background fill: [label]. The color still identifies the
// category, tag or level, but the text reads on any terminal.
func bracketChip(label string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(brightColor(fg)).
		Bold(true).
		Render("[" + label + "]")
}

// categoryChip renders a category name as a colored bracket label.
func categoryChip(name, title string) string {
	return bracketChip(title, categoryColor(name))
}

// tagChips renders tags as bright bracket labels, wrapped to the
// given width. Each tag takes the next color from the palette.
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
		c := bracketChip(name, categoryPalette[i%len(categoryPalette)])
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

// levelColor is the dot color for a usefulness level. Advanced is
// light gray (7), not dark gray: a dark dot on a dark terminal is
// barely visible.
func levelColor(level int) lipgloss.Color {
	switch level {
	case db.LevelEssential:
		return lipgloss.Color("10")
	case db.LevelCommon:
		return lipgloss.Color("11")
	case db.LevelAdvanced:
		return lipgloss.Color("7")
	default:
		return dimColor
	}
}

// levelChip renders the active usefulness filter as a bright bracket
// label, e.g. [● Essential]. No background fill, so it reads on any
// terminal — the same style as the tag chips.
func levelChip(level int) string {
	if level == 0 {
		return ""
	}
	return bracketChip("● "+db.LevelName(level), levelColor(level))
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
	rightText := fmt.Sprintf("%d entries", m.total)
	// At 60 columns or less the sidebar is hidden, so the header
	// carries the active category name instead.
	if m.width <= 60 && m.activeSide >= 0 && m.activeSide < len(m.sides) {
		rightText = m.sides[m.activeSide].title + " · " + rightText
	}
	chip := levelChip(m.level)
	rightW := runeLen(rightText)
	if chip != "" {
		rightW += 2 + lipgloss.Width(chip)
	}
	// Shrink the right side (dropping the chip first) until it fits.
	avail := m.width - lipgloss.Width(left) - 1
	if rightW > avail {
		chip = ""
		rightW = runeLen(rightText)
	}
	if rightW > avail && avail > 4 {
		rightText = truncate(rightText, avail)
	}
	right := dimStyle.Render(rightText)
	if chip != "" {
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
	// At 60 columns or less there is no room for a sidebar: the list takes
	// the whole width and the header shows the active category.
	if m.width <= 60 {
		return 0, 0, m.width
	}
	sidebarW = 24
	detailW = 0
	if m.width >= 90 {
		// The detail pane gets about 45% of the width, bounded so
		// the results list keeps at least 30 columns.
		detailW = m.width * 45 / 100
		if maxD := m.width - sidebarW - 2 - 30; detailW > maxD {
			detailW = maxD
		}
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
	list := m.viewList(listW, bodyH)
	if sidebarW == 0 {
		return list
	}
	sidebar := m.viewSidebar(sidebarW-2, bodyH-2)
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
		// The row is built one cell narrower than the box: a row exactly
		// as wide as the box would wrap. Every row carries a 2-cell
		// gutter: the active row has the same bold accent arrow as the
		// result list, the others two spaces — no background fill.
		w := innerW - 1
		suffix := fmt.Sprintf("(%d)", item.count)
		label := truncate(item.title, max(1, w-len(suffix)-4))
		gap := w - 4 - lipgloss.Width(label) - len(suffix)
		// Every row carries its category color: a colored dot, and a
		// colored label. The active row's label is accent bold.
		color := categoryColor(item.name)
		if item.kind == sideFavorites {
			color = favColor
		} else if item.kind == sideAll {
			color = accentColor
		}
		dot := lipgloss.NewStyle().Foreground(color).Render("●")
		var gutter, labelStyled string
		if i == m.activeSide {
			gutter = arrowStyle.Render("> ")
			labelStyled = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(label)
		} else {
			gutter = "  "
			labelStyled = lipgloss.NewStyle().Foreground(color).Render(label)
		}
		line := gutter + dot + " " + labelStyled + strings.Repeat(" ", max(0, gap)) + dimStyle.Render(suffix)
		lines = append(lines, line)
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
		// A silent empty list is usually the level filter, not the
		// search: say so, instead of letting the user think the
		// search found nothing.
		if m.level != 0 {
			lines = append(lines, dimStyle.Render("Level filter is "+db.LevelName(m.level)+" — press f to change it."))
		}
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

// viewRowLines renders one entry as a single line plus a blank
// separator. Every row carries a 2-cell gutter: the selected row has a
// bold accent arrow that moves with the cursor, the others two spaces,
// so rows never shift when the selection moves. There is no background
// fill anywhere — the arrow is the cursor. The description is not
// shown here; it lives in the detail pane.
func (m Model) viewRowLines(e db.Entry, selected bool, listW int) []string {
	star := ""
	if e.Favorite {
		star = " " + favStyle.Render("★")
	}
	// Truncate the command first (plain text), then style it: cutting a
	// styled string would break its escape codes.
	cmd := truncate(e.Command, max(1, listW-7))
	dot := lipgloss.NewStyle().Foreground(levelColor(e.Level)).Render("●")
	var gutter, cmdStyled string
	if selected {
		gutter = arrowStyle.Render("> ")
		cmdStyled = selectedCmdStyle.Render(cmd)
	} else {
		gutter = "  "
		cmdStyled = titleStyle.Render(cmd)
	}
	return []string{gutter + dot + " " + cmdStyled + star, ""}
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
		lines = append(lines, titleStyle.Render("Command:"))
		for _, ln := range wrapText(e.Command, innerW) {
			lines = append(lines, titleStyle.Render(ln))
		}
		if strings.TrimSpace(e.Description) != "" {
			lines = append(lines, "")
			lines = append(lines, titleStyle.Render("Description:"))
			lines = append(lines, wrapText(e.Description, innerW)...)
		}
		if strings.TrimSpace(e.Example) != "" {
			lines = append(lines, "")
			lines = append(lines, titleStyle.Render("Example:"))
			lines = append(lines, wrapText(e.Example, innerW)...)
		}
		if strings.TrimSpace(e.Tags) != "" {
			lines = append(lines, "")
			lines = append(lines, titleStyle.Render("Tags:"))
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
// search mode typing goes to the search box, so the footer first says
// the keys need Esc once — that is the line the user actually reads.
func footerHints(listMode bool) []keyHint {
	if listMode {
		return []keyHint{
			{"Enter", "copy"}, {"/", "search"}, {"f", "filter"},
			{"s", "favorite"}, {"a", "add"}, {"C", "category"},
			{"d", "delete"}, {"E", "export"}, {"I", "import"},
			{"?", "help"}, {"q", "quit"},
		}
	}
	return []keyHint{
		{"Esc", "once → keys work"}, {"Enter", "copy"}, {"Esc", "list"},
		{"f", "filter"}, {"s", "favorite"}, {"a", "add"},
		{"C", "category"}, {"d", "delete"}, {"E", "export"},
		{"I", "import"}, {"?", "help"}, {"Ctrl+C", "quit"},
	}
}

// footerHintsShort is the compact key list for narrow terminals.
func footerHintsShort(listMode bool) []keyHint {
	if listMode {
		return []keyHint{
			{"⏎", "copy"}, {"/", "search"}, {"f", "filter"},
			{"s", "fav"}, {"a", "add"}, {"C", "cat"},
			{"d", "del"}, {"E", "exp"}, {"I", "imp"},
			{"?", "help"}, {"q", "quit"},
		}
	}
	return []keyHint{
		{"Esc", "once → keys"}, {"⏎", "copy"}, {"Esc", "list"},
		{"f", "filter"}, {"s", "fav"}, {"a", "add"}, {"C", "cat"},
		{"d", "del"}, {"E", "exp"}, {"I", "imp"}, {"?", "help"},
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
		// It still carries the Esc note in search mode, and the
		// export/import keys in list mode.
		fallback := "Enter copy · E export · I import · a add · ? help · q quit"
		if m.focus != focusList {
			fallback = "Esc once → keys work · Enter copy · ? help"
		}
		footer = dimStyle.Render(truncate(fallback, avail))
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
	// fit in a 24-row terminal, or its top lines scroll off. Lines are
	// truncated to the terminal width so narrow screens stay readable.
	plain := []string{
		"cheat-terminal — Keyboard shortcuts",
		"",
		"MODES — SEARCH: typing goes to the search box.",
		"  Press Esc once to switch to list mode, where keys work.",
		"  LIST: single keys work directly. Esc or / back to search.",
		"",
		"SEARCH — ↑↓ move · Tab category · Enter copy",
		"  Esc clears the query, then list mode · Ctrl+C quit",
		"",
		"LIST — ↑↓ j k move · Enter / c copy · s favorite · f filter",
		"  a add · C category · d delete · E export · I import · q quit",
		"",
		"ADD (a) — Command → Description → Category → Tags → Level",
		"  New category name creates it · Level 1/2/3 (empty = 2)",
		"EXPORT (E) — scope (all or a category), then filename",
		"IMPORT (I) — file path, then category (empty = from file)",
		"DELETE (d) — y deletes, n / Esc keeps it",
		"",
		"Esc / ? closes this help.",
	}
	headers := map[int]bool{2: true, 6: true, 9: true, 12: true}
	// Truncate to the terminal width (box border + padding take 6).
	maxW := max(10, m.width-10)
	var lines []string
	for i, p := range plain {
		p = truncate(p, maxW)
		switch {
		case i == 0:
			lines = append(lines, headerTitleStyle.Render(p))
		case headers[i]:
			lines = append(lines, titleStyle.Render(p))
		case i == len(plain)-1:
			lines = append(lines, dimStyle.Render(p))
		default:
			lines = append(lines, p)
		}
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
// add-category prompt, the delete confirmation, or the export/import
// prompts.
func (m Model) viewPrompt() string {
	switch m.prompt {
	case promptAdd:
		return m.viewAddForm()
	case promptCategory:
		return m.viewCategoryForm()
	case promptDelete:
		return m.viewDeleteConfirm()
	case promptExport:
		return m.viewExportForm()
	case promptImport:
		return m.viewImportForm()
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
	if m.formErr != "" {
		lines = append(lines, errStyle.Render(truncate(m.formErr, w)))
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

func (m Model) viewExportForm() string {
	w := m.promptWidth()
	var title, hint string
	if m.exportStep == 0 {
		title = "Export entries — step 1 of 2: Scope"
		hint = "All, or a category name (Enter = all)."
	} else {
		title = "Export entries — step 2 of 2: File"
		hint = "Filename (Enter = " + exportDefaultFilename(m.exportScope) + ")."
	}
	lines := []string{
		headerTitleStyle.Render(title),
		"",
		dimStyle.Render(truncate(hint, w)),
		dimStyle.Render(truncate("Give it to an AI to enrich, then import it back.", w)),
	}
	if m.exportStep == 0 {
		lines = append(lines, dimStyle.Render(truncate("Existing: "+m.categoryTitles(), w)))
	} else if m.exportScope != "" {
		lines = append(lines, dimStyle.Render(truncate("Category: "+titleCaseName(m.exportScope), w)))
	}
	lines = append(lines, "")
	lines = append(lines, m.formInput.View())
	if m.formErr != "" {
		lines = append(lines, errStyle.Render(truncate(m.formErr, w)))
	}
	lines = append(lines, "")
	if m.exportStep == 0 {
		lines = append(lines, dimStyle.Render("Enter next step · Esc cancel"))
	} else {
		lines = append(lines, dimStyle.Render("Enter export · Esc cancel"))
	}
	return m.promptBox(lines)
}

func (m Model) viewImportForm() string {
	w := m.promptWidth()
	var title, hint string
	if m.importStep == 0 {
		title = "Import entries — step 1 of 2: File"
		hint = "Path to a JSON file in seed format."
	} else {
		title = "Import entries — step 2 of 2: Category"
		hint = "New or existing name — empty uses the file's categories."
	}
	lines := []string{
		headerTitleStyle.Render(title),
		"",
		dimStyle.Render(truncate(hint, w)),
	}
	if m.importStep == 1 {
		lines = append(lines, dimStyle.Render(truncate("Existing: "+m.categoryTitles(), w)))
	}
	lines = append(lines, "")
	lines = append(lines, m.formInput.View())
	if m.formErr != "" {
		lines = append(lines, errStyle.Render(truncate(m.formErr, w)))
	}
	lines = append(lines, "")
	if m.importStep == 0 {
		lines = append(lines, dimStyle.Render("Enter next step · Esc cancel"))
	} else {
		lines = append(lines, dimStyle.Render("Enter import · Esc cancel"))
	}
	return m.promptBox(lines)
}

// runeLen counts display cells (runes), unlike len() which counts bytes.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
