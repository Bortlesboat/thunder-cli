package tui

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Thunder-Compute/thunder-cli/tui/theme"
)

const (
	reportSubjectLimit = 200
	reportDetailsLimit = 10000
	reportMaxWidth     = 76
	reportMinWidth     = 30
	// The details editor's rows while the terminal height is unknown, and the
	// fewest it gets on a short terminal.
	reportEditorRows    = 6
	reportEditorMinRows = 3
	// The space between the step and the summary box beside it, and the
	// narrowest box worth showing there.
	reportColumnGap   = 2
	reportMinBoxWidth = 26
)

type reportStep int

const (
	reportStepType reportStep = iota
	reportStepSummary
	reportStepDetails
	reportStepConfirm
)

var reportStepNames = []string{"Type", "Summary", "Details", "Confirm"}

type reportKind struct {
	value, label, reviewName        string
	summaryQuestion, summaryExample string
}

var reportKinds = []reportKind{
	{
		value: "bug", label: "Report a problem", reviewName: "Bug report",
		summaryQuestion: "What is the problem?", summaryExample: "Can't connect to my instance",
	},
	{
		value: "feedback", label: "Suggest an improvement", reviewName: "Product feedback",
		summaryQuestion: "What would you like to improve?", summaryExample: "Show when an instance is ready",
	},
}

type reportAction int

const (
	reportSend reportAction = iota
	reportEdit
	reportCancel
)

var reportActionLabels = map[reportAction]string{
	reportSend:   "✓ Send report",
	reportEdit:   "✎ Edit report",
	reportCancel: "✗ Cancel",
}

// ReportInput is a report's kind ("bug" or "feedback") and text. RunReport
// prompts for each empty field and uses supplied fields as given.
type ReportInput struct {
	Kind    string
	Subject string
	Details string
}

// ReportField is supplied context shown in the review. Empty values are omitted.
type ReportField struct {
	Label string
	Value string
}

type reportModel struct {
	step         reportStep
	cursor       int
	kind         int
	subject      string
	details      string
	subjectInput textinput.Model
	detailsInput textarea.Model
	skipped      map[reportStep]bool
	context      []ReportField
	validation   string
	// terminalWidth is the whole terminal's width, which the summary box
	// reaches out to; width is the layout width, clamped to
	// [reportMinWidth, reportMaxWidth].
	terminalWidth int
	width         int
	// height is the terminal height, or 0 while it is unknown.
	height   int
	done     bool
	quitting bool
	styles   PanelStyles
}

func newReportModel(input ReportInput, context []ReportField, confirm bool) reportModel {
	subjectInput := textinput.New()
	subjectInput.Prompt = "▶ "
	subjectInput.PromptStyle = CursorStyle()
	subjectInput.TextStyle = SelectedStyle()
	subjectInput.PlaceholderStyle = SubtleTextStyle()

	detailsInput := textarea.New()
	detailsInput.ShowLineNumbers = false
	detailsInput.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "▶ "
		}
		return "  "
	})
	// The defaults paint the cursor line and prompt in fixed palette colours,
	// which theme.go rules out.
	detailsInput.FocusedStyle.CursorLine = lipgloss.NewStyle()
	detailsInput.FocusedStyle.Prompt = lipgloss.NewStyle()
	detailsInput.SetHeight(reportEditorRows)
	// A blinking cursor redraws its line twice a second, and terminals clear a
	// mouse selection when the screen changes under it.
	subjectInput.Cursor.SetMode(cursor.CursorStatic)
	detailsInput.Cursor.SetMode(cursor.CursorStatic)

	m := reportModel{
		subject:      input.Subject,
		details:      input.Details,
		subjectInput: subjectInput,
		detailsInput: detailsInput,
		skipped: map[reportStep]bool{
			reportStepType:    input.Kind != "",
			reportStepSummary: input.Subject != "",
			reportStepDetails: input.Details != "",
			reportStepConfirm: !confirm,
		},
		context: context,
		styles:  NewPanelStyles(),
	}
	for i, kind := range reportKinds {
		if kind.value == input.Kind {
			m.kind = i
		}
	}
	m.setWidth(80)
	if first := m.nextStep(-1); first >= 0 {
		m.enter(first)
	} else {
		m.done = true
	}
	m.fitEditor()
	return m
}

func (m *reportModel) setWidth(terminalWidth int) {
	m.terminalWidth = max(reportMinWidth, terminalWidth)
	m.width = min(m.terminalWidth, reportMaxWidth)
	m.subjectInput.Width = m.width - 3
}

// nextStep returns the first step after from that the user fills in, or -1.
func (m reportModel) nextStep(from reportStep) reportStep {
	for step := from + 1; step <= reportStepConfirm; step++ {
		if !m.skipped[step] {
			return step
		}
	}
	return -1
}

// prevStep returns the last step before from that the user fills in, or -1.
func (m reportModel) prevStep(from reportStep) reportStep {
	for step := from - 1; step >= reportStepType; step-- {
		if !m.skipped[step] {
			return step
		}
	}
	return -1
}

// editStep is where "Edit report" returns to: the first text the user wrote,
// otherwise the type menu.
func (m reportModel) editStep() (reportStep, bool) {
	for _, step := range []reportStep{reportStepSummary, reportStepDetails, reportStepType} {
		if !m.skipped[step] {
			return step, true
		}
	}
	return 0, false
}

func (m reportModel) confirmActions() []reportAction {
	if _, ok := m.editStep(); ok {
		return []reportAction{reportSend, reportEdit, reportCancel}
	}
	return []reportAction{reportSend, reportCancel}
}

func (m *reportModel) enter(step reportStep) tea.Cmd {
	m.step = step
	m.validation = ""
	m.subjectInput.Blur()
	m.detailsInput.Blur()
	switch step {
	case reportStepType:
		m.cursor = m.kind
	case reportStepSummary:
		m.subjectInput.Placeholder = "Example: " + reportKinds[m.kind].summaryExample
		return m.subjectInput.Focus()
	case reportStepDetails:
		return m.detailsInput.Focus()
	case reportStepConfirm:
		m.cursor = 0
	}
	return nil
}

func (m reportModel) advance() (tea.Model, tea.Cmd) {
	next := m.nextStep(m.step)
	if next < 0 {
		m.done = true
		return m, tea.Quit
	}
	return m, m.enter(next)
}

func (m reportModel) back() (tea.Model, tea.Cmd) {
	prev := m.prevStep(m.step)
	if prev < 0 {
		m.quitting = true
		return m, tea.Quit
	}
	return m, m.enter(prev)
}

func (m reportModel) Init() tea.Cmd {
	return nil
}

func (m reportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	model := next.(reportModel)
	model.fitEditor()
	return model, cmd
}

// fitEditor gives the details editor the room the step leaves it. Update calls
// it after every message because that room depends on the terminal size and on
// everything else the step shows, such as a validation error.
func (m *reportModel) fitEditor() {
	if m.step != reportStepDetails || m.done || m.quitting {
		return
	}
	width, rows := m.width, reportEditorRows
	if m.sideBySide() {
		width = m.mainWidth()
	}
	if body := m.bodyHeight(); body > 0 {
		rows = body
		if !m.sideBySide() {
			rows -= lipgloss.Height(m.summaryLine()) + 1
		}
	}
	oldWidth, oldHeight := m.detailsInput.Width(), m.detailsInput.Height()
	m.detailsInput.SetWidth(width)
	m.detailsInput.SetHeight(max(rows, reportEditorMinRows))
	if m.detailsInput.Width() != oldWidth || m.detailsInput.Height() != oldHeight {
		// Resizing leaves the scroll position alone; Update scrolls the cursor
		// back into view.
		m.detailsInput, _ = m.detailsInput.Update(nil)
	}
}

func (m reportModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.setWidth(msg.Width)
		}
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			return m.back()
		}
		switch m.step {
		case reportStepType, reportStepConfirm:
			return m.updateMenu(msg)
		case reportStepSummary:
			if msg.String() == "enter" {
				return m.submitSubject()
			}
		case reportStepDetails:
			if msg.String() == "ctrl+d" {
				return m.submitDetails()
			}
		}
	}
	var cmd tea.Cmd
	switch m.step {
	case reportStepSummary:
		m.subjectInput, cmd = m.subjectInput.Update(msg)
	case reportStepDetails:
		m.detailsInput, cmd = m.detailsInput.Update(msg)
	}
	return m, cmd
}

func (m reportModel) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := len(reportKinds)
	if m.step == reportStepConfirm {
		options = len(m.confirmActions())
	}
	switch msg.String() {
	case "q", "Q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < options-1 {
			m.cursor++
		}
	case "enter":
		if m.step == reportStepType {
			m.kind = m.cursor
			return m.advance()
		}
		switch m.confirmActions()[m.cursor] {
		case reportSend:
			m.done = true
			return m, tea.Quit
		case reportEdit:
			step, _ := m.editStep()
			return m, m.enter(step)
		default:
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m reportModel) submitSubject() (tea.Model, tea.Cmd) {
	subject := strings.TrimSpace(m.subjectInput.Value())
	if n := utf8.RuneCountInString(subject); n < 1 || n > reportSubjectLimit {
		m.validation = "Summary must be 1–200 characters."
		return m, nil
	}
	m.subject = subject
	return m.advance()
}

func (m reportModel) submitDetails() (tea.Model, tea.Cmd) {
	details := strings.TrimSpace(m.detailsInput.Value())
	if n := utf8.RuneCountInString(details); n < 1 || n > reportDetailsLimit {
		m.validation = "Details must be 1–10,000 characters."
		return m, nil
	}
	m.details = details
	return m.advance()
}

func (m reportModel) result() ReportInput {
	return ReportInput{Kind: reportKinds[m.kind].value, Subject: m.subject, Details: m.details}
}

func (m reportModel) View() string {
	if m.done || m.quitting {
		return ""
	}
	content := m.header() + m.body() + m.footer()
	if m.sideBySide() && (m.step == reportStepDetails || m.step == reportStepConfirm) {
		content = m.withSummaryBox(content)
	}
	return "\n" + content
}

func (m reportModel) header() string {
	header := TitleStyle().Render("⚡ Report to Thunder Support") + "\n\n" + m.progressView() + "\n"
	if m.step == reportStepConfirm && !m.sideBySide() {
		// The summary box beside the menu rises into the blank row below the
		// progress.
		return header
	}
	return header + "\n"
}

func (m reportModel) helpText(step reportStep) string {
	switch step {
	case reportStepSummary:
		return "Enter: Continue  Esc: Back  Ctrl+C: Quit"
	case reportStepDetails:
		finish := "Review"
		if m.skipped[reportStepConfirm] {
			finish = "Done"
		}
		return "Enter: New line  Ctrl+D: " + finish + "  Esc: Back  Ctrl+C: Quit"
	case reportStepConfirm:
		return "↑/↓: Navigate  Enter: Confirm  Esc: Back  Q: Quit"
	}
	return "↑/↓: Navigate  Enter: Select  Esc: Back  Q: Quit"
}

func (m reportModel) footer() string {
	return m.validationView() + "\n" + m.styles.Help.Render(m.helpText(m.step))
}

// bodyHeight is how many rows the step gets between the header and the
// controls, or 0 while the terminal height is unknown. Bubble Tea drops the top
// lines of a view taller than the terminal, so the body never takes more. The
// first row is the blank line above the header. The review grows only as far
// as its summary box has something to show.
func (m reportModel) bodyHeight() int {
	if m.height <= 0 {
		return 0
	}
	chrome := strings.Count(m.header(), "\n") + lipgloss.Height(m.footer())
	rows := m.height - 1 - chrome
	if m.step == reportStepConfirm {
		width, actions := m.summaryBoxWidth(), len(m.confirmActions())
		if m.sideBySide() {
			// The box also runs beside the header and the controls.
			rows = min(rows, max(actions, m.summaryBoxRows(width, rows+chrome, true)-chrome))
		} else {
			// The menu starts a row below the box's top edge.
			rows = min(rows, max(actions+1, m.summaryBoxRows(width, rows, true)))
		}
	}
	return max(reportEditorMinRows, rows)
}

func (m reportModel) body() string {
	switch m.step {
	case reportStepType:
		var s strings.Builder
		s.WriteString("What would you like to send?\n\n")
		for i, option := range reportKinds {
			s.WriteString(m.menuRow(i, option.label))
		}
		return s.String()
	case reportStepSummary:
		return reportKinds[m.kind].summaryQuestion + "\n\n" + m.subjectInput.View() + "\n"
	case reportStepDetails:
		if m.sideBySide() {
			return m.detailsInput.View() + "\n"
		}
		return m.summaryLine() + "\n\n" + m.detailsInput.View() + "\n"
	}
	menu := m.reviewMenu()
	if m.sideBySide() {
		// Keep the controls level with the box's bottom edge.
		return menu + strings.Repeat("\n", max(0, m.bodyHeight()-len(m.confirmActions())))
	}
	// Without room beside the title and the controls, the box stays on the right
	// between them, with the menu level with its first line.
	actions := "\n" + strings.TrimSuffix(menu, "\n")
	box := m.summaryBox(m.summaryBoxWidth(), m.bodyHeight(), true)
	return lipgloss.JoinHorizontal(lipgloss.Top, actions, strings.Repeat(" ", reportColumnGap), box) + "\n"
}

func (m reportModel) reviewMenu() string {
	var menu strings.Builder
	for i, action := range m.confirmActions() {
		menu.WriteString(m.menuRow(i, reportActionLabels[action]))
	}
	return menu.String()
}

func (m reportModel) progressView() string {
	var entries []string
	for i, name := range reportStepNames {
		step := reportStep(i)
		switch {
		case step == reportStepConfirm && m.skipped[step]:
			// Without a review the flow simply ends; there is nothing to show.
		case step == m.step:
			entries = append(entries, m.styles.Selected.Render("["+name+"]"))
		case step < m.step || m.skipped[step]:
			entries = append(entries, "[✓ "+name+"]")
		default:
			entries = append(entries, "["+name+"]")
		}
	}
	return strings.Join(entries, " → ")
}

func (m reportModel) menuRow(index int, label string) string {
	if index == m.cursor {
		return m.styles.Cursor.Render("▶ ") + m.styles.Selected.Render(label) + "\n"
	}
	return "  " + label + "\n"
}

func (m reportModel) validationView() string {
	if m.validation == "" {
		return ""
	}
	return ErrorStyle().Render("✗ "+m.validation) + "\n"
}

// mainWidth is the column left of the summary box: wide enough for the step's
// progress and controls, so the box never runs into them.
func (m reportModel) mainWidth() int {
	return max(lipgloss.Width(m.progressView()), lipgloss.Width(m.helpText(m.step)))
}

// sideBySide reports whether the summary box fits beside the whole step, from
// the title to the controls.
func (m reportModel) sideBySide() bool {
	return m.terminalWidth >= m.mainWidth()+reportColumnGap+reportMinBoxWidth
}

// summaryLine stands in for the summary box above the details editor when the
// box does not fit beside it.
func (m reportModel) summaryLine() string {
	return SubtleTextStyle().Render(ansi.Wrap("Summary: "+printable(m.subject), m.width, ""))
}

// withSummaryBox runs the summary box down the right of the step, from the
// title to the controls, and out to the terminal's right edge.
func (m reportModel) withSummaryBox(content string) string {
	main := theme.Body().Width(m.mainWidth()).Render(content)
	height := 0
	if m.height > 0 {
		height = lipgloss.Height(main)
	}
	box := m.summaryBox(m.summaryBoxWidth(), height, m.step == reportStepConfirm)
	return lipgloss.JoinHorizontal(lipgloss.Top, main, strings.Repeat(" ", reportColumnGap), box)
}

// summaryBoxWidth runs the summary box from beside the step, or from beside the
// review's menu when it sits between the title and the controls, out to the
// terminal's right edge.
func (m reportModel) summaryBoxWidth() int {
	if m.sideBySide() {
		return m.terminalWidth - m.mainWidth() - reportColumnGap
	}
	return m.terminalWidth - lipgloss.Width(m.reviewMenu()) - reportColumnGap
}

// summaryBox lists the report's type, summary, supplied context and, on the
// review, its description. Given a height, the box fills exactly that many rows.
func (m reportModel) summaryBox(width, height int, withDescription bool) string {
	lines, padded := m.summaryBoxLines(width, height, withDescription)
	box := m.styles.Panel.UnsetMargins().Width(width - 2)
	if !padded {
		box = box.UnsetPaddingTop().UnsetPaddingBottom()
	}
	if height > 0 {
		box = box.Height(max(height, 3) - 2)
	}
	return box.Render(strings.Join(lines, "\n"))
}

// summaryBoxRows is how many of at most height rows the summary box needs to
// show what fits.
func (m reportModel) summaryBoxRows(width, height int, withDescription bool) int {
	lines, padded := m.summaryBoxLines(width, height, withDescription)
	if padded {
		return len(lines) + 4
	}
	return len(lines) + 2
}

// summaryBoxLines lays out the summary box's fields in at most height rows, or
// at their natural size for a height of 0. To fit, the box gives up its padding,
// then puts labels beside their values, before it cuts the end of the
// description, which comes last for that reason.
func (m reportModel) summaryBoxLines(width, height int, withDescription bool) (lines []string, padded bool) {
	fields := []ReportField{
		{Label: "Type", Value: reportKinds[m.kind].reviewName},
		{Label: "Summary", Value: m.subject},
	}
	for _, field := range m.context {
		if field.Value != "" {
			fields = append(fields, field)
		}
	}
	if withDescription {
		fields = append(fields, ReportField{Label: "Description", Value: m.details})
	}
	// The border and the horizontal padding take six columns.
	textWidth := width - 6
	above := m.boxLines(fields, textWidth, false)
	if height <= 0 {
		return above, true
	}
	height = max(height, 3)
	switch {
	case len(above) <= height-4:
		return above, true
	case len(above) <= height-2:
		return above, false
	}
	inline := m.boxLines(fields, textWidth, true)
	if rows := height - 2; len(inline) > rows {
		// Count what is hidden, unless the count would crowd out the type and
		// summary.
		if rows < 3 {
			inline = inline[:rows]
		} else {
			keep := rows - 1
			inline = append(inline[:keep], SubtleTextStyle().Render(fmt.Sprintf("… %d more lines", len(inline)-keep)))
		}
	}
	return inline, false
}

// boxLines lays fields out for the summary box: each label above its value
// with a blank line between fields or, inline, beside its value.
func (m reportModel) boxLines(fields []ReportField, width int, inline bool) []string {
	label := m.styles.Label
	var lines []string
	for i, field := range fields {
		value := printable(field.Value)
		if inline {
			lines = append(lines, strings.Split(ansi.Wrap(label.Render(field.Label+":")+" "+value, width, ""), "\n")...)
			continue
		}
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, label.Render(field.Label))
		lines = append(lines, strings.Split(ansi.Wrap(value, width, ""), "\n")...)
	}
	return lines
}

// printable keeps text read from flags or files from sending control sequences
// to the terminal while it is displayed. The report itself is sent unchanged.
func printable(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, text)
}

// RunReport walks the user through each report step whose field is empty in
// input and, when confirm is set, a review that sends, edits or cancels the
// report. It returns ErrCancelled when the user quits.
func RunReport(input ReportInput, context []ReportField, confirm bool) (ReportInput, error) {
	InitCommonStyles(os.Stdout)
	model := newReportModel(input, context, confirm)
	if model.done {
		return model.result(), nil
	}
	// The steps fill the terminal, and a terminal re-wraps full-width lines when
	// it narrows, which strands old frames in the scrollback of an inline
	// program. The alternate screen is redrawn whole on every resize, and the
	// shell's screen comes back untouched when the wizard ends.
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return ReportInput{}, err
	}
	final := result.(reportModel)
	if !final.done {
		return ReportInput{}, ErrCancelled
	}
	return final.result(), nil
}
