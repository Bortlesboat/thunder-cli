package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func sendReportMsgs(model reportModel, msgs ...tea.Msg) reportModel {
	for _, msg := range msgs {
		next, _ := model.Update(msg)
		model = next.(reportModel)
	}
	return model
}

func reportKey(keyType tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: keyType} }

func reportText(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}

func TestReportPreservesPastesAndValidatesBeforeReview(t *testing.T) {
	for _, tc := range []struct {
		name     string
		step     reportStep
		length   int
		wantNext bool
	}{
		{"summary at limit continues", reportStepSummary, 200, true},
		{"oversized summary remains editable", reportStepSummary, 201, false},
		{"details at limit continue", reportStepDetails, 10000, true},
		{"oversized details remain editable", reportStepDetails, 10001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newReportModel(ReportInput{Kind: "bug"}, nil, true)
			finish := reportKey(tea.KeyEnter)
			if tc.step == reportStepDetails {
				model.enter(reportStepDetails)
				finish = reportKey(tea.KeyCtrlD)
			}
			text := strings.Repeat("界", tc.length)
			model = sendReportMsgs(model, reportText(text), finish)
			value := model.subjectInput.Value()
			if tc.step == reportStepDetails {
				value = model.detailsInput.Value()
			}
			assert.Equal(t, text, value)
			assert.Equal(t, tc.wantNext, model.step > tc.step)
			assert.Equal(t, !tc.wantNext, model.validation != "")
		})
	}
}

func TestReportFlowKeepsTextWhenGoingBackAndEditing(t *testing.T) {
	model := newReportModel(ReportInput{}, nil, true)
	model = sendReportMsgs(model,
		reportKey(tea.KeyDown), reportKey(tea.KeyEnter),
		reportText("Show readiness"), reportKey(tea.KeyEnter),
		reportText("A Ready column"), reportKey(tea.KeyEsc),
	)
	assert.Equal(t, reportStepSummary, model.step)
	assert.Equal(t, "Show readiness", model.subjectInput.Value())
	assert.Equal(t, "A Ready column", model.detailsInput.Value())

	model = sendReportMsgs(model, reportKey(tea.KeyEnter), reportKey(tea.KeyCtrlD))
	assert.Equal(t, reportStepConfirm, model.step)
	assert.Contains(t, model.View(), "Product feedback")
	model = sendReportMsgs(model, reportKey(tea.KeyDown), reportKey(tea.KeyEnter))
	assert.Equal(t, reportStepSummary, model.step, "Edit report returns to the summary")

	model = sendReportMsgs(model, reportKey(tea.KeyEnter), reportKey(tea.KeyCtrlD), reportKey(tea.KeyEnter))
	assert.True(t, model.done)
	assert.Equal(t, ReportInput{Kind: "feedback", Subject: "Show readiness", Details: "A Ready column"}, model.result())
}

func TestReportPresetsSkipTheirSteps(t *testing.T) {
	model := newReportModel(ReportInput{Kind: "bug", Subject: "Disk full", Details: "Evidence"}, []ReportField{{Label: "Instance", Value: "42"}}, true)
	assert.Equal(t, reportStepConfirm, model.step)
	assert.Equal(t, []reportAction{reportSend, reportCancel}, model.confirmActions(), "nothing is left to edit")
	view := model.View()
	assert.Contains(t, view, "[✓ Details]")
	assert.Contains(t, view, "42")

	assert.True(t, sendReportMsgs(model, reportKey(tea.KeyEsc)).quitting)
	assert.True(t, newReportModel(ReportInput{Kind: "bug", Subject: "Disk full", Details: "Evidence"}, nil, false).done)
}

func TestReportFillsTheTerminalWithHeaderVisible(t *testing.T) {
	for _, tc := range []struct {
		step          reportStep
		width, height int
		sideBySide    bool
		// The terminal is taller than the whole review.
		roomToSpare bool
	}{
		{reportStepDetails, 100, 11, true, false}, // box without padding
		{reportStepDetails, 120, 30, true, false}, // padded box
		{reportStepDetails, 80, 14, false, false}, // summary line instead of the box
		{reportStepConfirm, 80, 11, true, false},  // inline labels, description cut
		{reportStepConfirm, 60, 14, false, false}, // inline labels, description cut
		{reportStepConfirm, 120, 30, true, true},  // inline labels
		{reportStepConfirm, 120, 60, true, true},  // labels above values, padded
		{reportStepConfirm, 60, 60, false, true},  // labels above values, padded
	} {
		t.Run(fmt.Sprintf("%s at %dx%d", reportStepNames[tc.step], tc.width, tc.height), func(t *testing.T) {
			model := newReportModel(ReportInput{Kind: "bug", Subject: "Disk full"}, []ReportField{{Label: "Instance", Value: "42"}}, true)
			model = sendReportMsgs(model, tea.WindowSizeMsg{Width: tc.width, Height: tc.height}, reportText(strings.Repeat("line\n", 20)+"last"))
			if tc.step == reportStepConfirm {
				model = sendReportMsgs(model, reportKey(tea.KeyCtrlD))
			}
			assert.Equal(t, tc.step, model.step)
			assert.Equal(t, tc.sideBySide, model.frame().sideBySide)
			view := model.View()
			if tc.roomToSpare {
				assert.Less(t, lipgloss.Height(view), tc.height, "the review stops growing once the whole report shows")
			} else {
				assert.Equal(t, tc.height, lipgloss.Height(view), "Bubble Tea cuts the top of a taller view")
			}
			assert.Contains(t, view, "⚡ Report to Thunder Support")
			lines := strings.Split(view, "\n")
			bottom := len(lines) - 1
			if tc.sideBySide {
				assert.Contains(t, lines[1], "╭", "the summary box starts level with the title")
				assert.Contains(t, lines[bottom], "╰", "and ends level with the controls")
			}
			if tc.step == reportStepConfirm {
				if !tc.sideBySide {
					bottom = len(lines) - 3
					assert.Contains(t, lines[4], "╭", "the summary box starts right below the progress")
					assert.Contains(t, lines[5], "Send report")
					assert.Contains(t, lines[5], "│", "beside the menu")
					assert.Contains(t, lines[bottom], "╰", "and ends above the controls")
				}
				assert.Contains(t, view, "Description")
			}
			if tc.roomToSpare {
				last := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "last") })
				assert.LessOrEqual(t, bottom-last, 2, "the box ends at the description's last line and any padding")
				assert.NotContains(t, view, "more lines")
			}
			if tc.sideBySide || tc.step == reportStepConfirm {
				assert.Equal(t, tc.width, lipgloss.Width(view), "the summary box reaches the right edge")
			}
		})
	}
}
