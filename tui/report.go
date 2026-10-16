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

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/tui/theme"
)

const (
	reportMaxWidth = 76
	reportMinWidth = 30
	// The terminal width assumed until Bubble Tea reports the real one.
	reportDefaultTerminalWidth = 80
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

var reportActionLabels = []string{"✓ Send report", "✎ Edit report", "✗ Cancel"}

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
	step      reportStep
	cursor    int
	kindIndex int
	// subject and details are the accepted text: as supplied, or as submitted
	// from the inputs. Supplied text never goes into the inputs, whose SetValue
	// rewrites tabs and control characters and cuts details at 99 lines.
	subject      string
	details      string
	subjectInput textinput.Model
	detailsInput textarea.Model
	skipped      map[reportStep]bool
	confirm      bool
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
		},
		confirm: confirm,
		context: context,
		styles:  NewPanelStyles(),
	}
	for i, kind := range reportKinds {
		if kind.value == input.Kind {
			m.kindIndex = i
		}
	}
	m.setWidth(reportDefaultTerminalWidth)
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
	// The cursor takes a column after the last character.
	m.subjectInput.Width = m.width - lipgloss.Width(m.subjectInput.Prompt) - 1
}

// nextStep returns the first step after from that the user fills in, or -1.
func (m reportModel) nextStep(from reportStep) reportStep {
	last := reportStepDetails
	if m.confirm {
		last = reportStepConfirm
	}
	for step := from + 1; step <= last; step++ {
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
		m.cursor = m.kindIndex
	case reportStepSummary:
		m.subjectInput.Placeholder = "Example: " + reportKinds[m.kindIndex].summaryExample
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
	frame := m.frame()
	width, rows := m.width, reportEditorRows
	if frame.sideBySide {
		width = frame.mainWidth
	}
	if frame.bodyRows > 0 {
		rows = frame.bodyRows
		if !frame.sideBySide {
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
			m.kindIndex = m.cursor
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
	if n := utf8.RuneCountInString(subject); n < 1 || n > api.MaxReportSubjectRunes {
		m.validation = fmt.Sprintf("Summary must be 1–%d characters.", api.MaxReportSubjectRunes)
		return m, nil
	}
	m.subject = subject
	return m.advance()
}

func (m reportModel) submitDetails() (tea.Model, tea.Cmd) {
	details := strings.TrimSpace(m.detailsInput.Value())
	if n := utf8.RuneCountInString(details); n < 1 || n > api.MaxReportDetailsRunes {
		m.validation = fmt.Sprintf("Details must be 1–%d characters.", api.MaxReportDetailsRunes)
		return m, nil
	}
	m.details = details
	return m.advance()
}

func (m reportModel) result() ReportInput {
	return ReportInput{Kind: reportKinds[m.kindIndex].value, Subject: m.subject, Details: m.details}
}

func (m reportModel) View() string {
	if m.done || m.quitting {
		return ""
	}
	frame := m.frame()
	content := frame.header + m.body(frame) + frame.footer
	if frame.sideBySide && (m.step == reportStepDetails || m.step == reportStepConfirm) {
		content = m.withSummaryBox(frame, content)
	}
	return "\n" + content
}

// reportFrame is the layout of one frame, worked out once for the view and for
// sizing the details editor.
type reportFrame struct {
	header, footer string
	// sideBySide puts the summary box beside the whole step, from the title to
	// the controls, with the step in the mainWidth columns on its left.
	sideBySide bool
	mainWidth  int
	boxWidth   int
	bodyRows   int
}

func (m reportModel) frame() reportFrame {
	progress, help := m.progressView(), m.helpText()
	// The step's column is wide enough for its progress and controls, so the
	// summary box beside it never runs into them.
	f := reportFrame{mainWidth: max(lipgloss.Width(progress), lipgloss.Width(help))}
	f.sideBySide = m.terminalWidth >= f.mainWidth+reportColumnGap+reportMinBoxWidth
	// The summary box runs from beside the step, or from beside the review's
	// menu when it sits between the title and the controls, out to the
	// terminal's right edge.
	if f.sideBySide {
		f.boxWidth = m.terminalWidth - f.mainWidth - reportColumnGap
	} else {
		f.boxWidth = m.terminalWidth - lipgloss.Width(m.reviewMenu()) - reportColumnGap
	}
	f.header = TitleStyle().Render("⚡ Report to Thunder Support") + "\n\n" + progress + "\n"
	// The summary box beside the review's menu rises into the blank row below
	// the progress.
	if m.step != reportStepConfirm || f.sideBySide {
		f.header += "\n"
	}
	f.footer = m.validationView() + "\n" + m.styles.Help.Render(help)
	f.bodyRows = m.bodyRows(f)
	return f
}

func (m reportModel) helpText() string {
	switch m.step {
	case reportStepSummary:
		return "Enter: Continue  Esc: Back  Ctrl+C: Quit"
	case reportStepDetails:
		finish := "Review"
		if !m.confirm {
			finish = "Done"
		}
		return "Enter: New line  Ctrl+D: " + finish + "  Esc: Back  Ctrl+C: Quit"
	case reportStepConfirm:
		return "↑/↓: Navigate  Enter: Confirm  Esc: Back  Q: Quit"
	}
	return "↑/↓: Navigate  Enter: Select  Esc: Back  Q: Quit"
}

// bodyRows is how many rows the step gets between the frame's header and
// controls, or 0 while the terminal height is unknown. Bubble Tea drops the top
// lines of a view taller than the terminal, so the body never takes more. The
// first row is the blank line above the header. The review grows only as far
// as its summary box has something to show.
func (m reportModel) bodyRows(frame reportFrame) int {
	if m.height <= 0 {
		return 0
	}
	chrome := strings.Count(frame.header, "\n") + lipgloss.Height(frame.footer)
	rows := m.height - 1 - chrome
	if m.step == reportStepConfirm {
		actions := len(m.confirmActions())
		if frame.sideBySide {
			// The box also runs beside the header and the controls.
			rows = min(rows, max(actions, m.summaryBoxRows(frame.boxWidth, rows+chrome, true)-chrome))
		} else {
			// The menu starts a row below the box's top edge.
			rows = min(rows, max(actions+1, m.summaryBoxRows(frame.boxWidth, rows, true)))
		}
	}
	return max(reportEditorMinRows, rows)
}

func (m reportModel) body(frame reportFrame) string {
	switch m.step {
	case reportStepType:
		var s strings.Builder
		s.WriteString("What would you like to send?\n\n")
		for i, option := range reportKinds {
			s.WriteString(m.menuRow(i, option.label))
		}
		return s.String()
	case reportStepSummary:
		return reportKinds[m.kindIndex].summaryQuestion + "\n\n" + m.subjectInput.View() + "\n"
	case reportStepDetails:
		if frame.sideBySide {
			return m.detailsInput.View() + "\n"
		}
		return m.summaryLine() + "\n\n" + m.detailsInput.View() + "\n"
	}
	menu := m.reviewMenu()
	if frame.sideBySide {
		// Keep the controls level with the box's bottom edge.
		return menu + strings.Repeat("\n", max(0, frame.bodyRows-len(m.confirmActions())))
	}
	// Without room beside the title and the controls, the box stays on the right
	// between them, with the menu level with its first line.
	actions := "\n" + strings.TrimSuffix(menu, "\n")
	box := m.summaryBox(frame.boxWidth, frame.bodyRows, true)
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
	names := reportStepNames
	if !m.confirm {
		names = names[:reportStepConfirm]
	}
	var entries []string
	for i, name := range names {
		step := reportStep(i)
		switch {
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

// summaryLine stands in for the summary box above the details editor when the
// box does not fit beside it.
func (m reportModel) summaryLine() string {
	return SubtleTextStyle().Render(ansi.Wrap("Summary: "+printable(m.subject), m.width, ""))
}

// withSummaryBox runs the summary box down the right of the step, from the
// title to the controls, and out to the terminal's right edge.
func (m reportModel) withSummaryBox(frame reportFrame, content string) string {
	main := theme.Body().Width(frame.mainWidth).Render(content)
	height := 0
	if m.height > 0 {
		height = lipgloss.Height(main)
	}
	box := m.summaryBox(frame.boxWidth, height, m.step == reportStepConfirm)
	return lipgloss.JoinHorizontal(lipgloss.Top, main, strings.Repeat(" ", reportColumnGap), box)
}

// summaryBox lists the report's type, summary, supplied context and, on the
// review, its description. Given a height, the box fills exactly that many rows.
func (m reportModel) summaryBox(width, height int, withDescription bool) string {
	lines, padded := m.summaryBoxLines(width, height, withDescription)
	box := m.styles.Panel.UnsetMargins()
	box = box.Width(width - box.GetHorizontalBorderSize())
	if !padded {
		box = box.UnsetPaddingTop().UnsetPaddingBottom()
	}
	if height > 0 {
		box = box.Height(max(height, box.GetVerticalBorderSize()+1) - box.GetVerticalBorderSize())
	}
	return box.Render(strings.Join(lines, "\n"))
}

// summaryBoxRows is how many of at most height rows the summary box needs to
// show what fits.
func (m reportModel) summaryBoxRows(width, height int, withDescription bool) int {
	lines, padded := m.summaryBoxLines(width, height, withDescription)
	box := m.styles.Panel.UnsetMargins()
	rows := len(lines) + box.GetVerticalBorderSize()
	if padded {
		rows += box.GetVerticalPadding()
	}
	return rows
}

// summaryBoxLines lays out the summary box's fields in at most height rows, or
// at their natural size for a height of 0. To fit, the box gives up its padding,
// then puts labels beside their values, before it cuts the end of the
// description, which comes last for that reason.
func (m reportModel) summaryBoxLines(width, height int, withDescription bool) (lines []string, padded bool) {
	fields := []ReportField{
		{Label: "Type", Value: reportKinds[m.kindIndex].reviewName},
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
	box := m.styles.Panel.UnsetMargins()
	textWidth := width - box.GetHorizontalFrameSize()
	above := m.boxLines(fields, textWidth, false)
	if height <= 0 {
		return above, true
	}
	// The rows inside the border, at least one.
	rows := max(height-box.GetVerticalBorderSize(), 1)
	switch {
	case len(above) <= rows-box.GetVerticalPadding():
		return above, true
	case len(above) <= rows:
		return above, false
	}
	inline := m.boxLines(fields, textWidth, true)
	if len(inline) > rows {
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
