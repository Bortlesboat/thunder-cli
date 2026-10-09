package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type reportInputModel struct {
	subject        textinput.Model
	details        textarea.Model
	editingDetails bool
	done           bool
	cancelled      bool
	validation     string
}

func (m reportInputModel) Init() tea.Cmd {
	if m.editingDetails {
		return textarea.Blink
	}
	return textinput.Blink
}
func (m reportInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if !m.editingDetails {
				if strings.TrimSpace(m.subject.Value()) == "" {
					m.validation = "Enter a short subject."
					return m, nil
				}
				m.editingDetails = true
				m.validation = ""
				m.subject.Blur()
				return m, m.details.Focus()
			}
		case "ctrl+d":
			if m.editingDetails {
				if strings.TrimSpace(m.details.Value()) == "" {
					m.validation = "Enter report details."
					return m, nil
				}
				m.done = true
				return m, tea.Quit
			}
		}
	}
	var command tea.Cmd
	if m.editingDetails {
		m.details, command = m.details.Update(msg)
	} else {
		m.subject, command = m.subject.Update(msg)
	}
	return m, command
}
func (m reportInputModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	if m.editingDetails {
		return TitleStyle().Render("Report details") + "\n" + m.details.View() + "\n" + m.validation + "\nCtrl+D to review · Esc to cancel\n"
	}
	return TitleStyle().Render("Report subject") + "\n" + m.subject.View() + "\n" + m.validation + "\nEnter to continue · Esc to cancel\n"
}

func RunReportInput(subject, details string) (string, string, error) {
	title := textinput.New()
	title.SetValue(subject)
	title.Width = 70
	body := textarea.New()
	body.CharLimit = 0
	body.SetValue(details)
	body.SetWidth(76)
	body.SetHeight(8)
	model := reportInputModel{subject: title, details: body, editingDetails: strings.TrimSpace(subject) != ""}
	if model.editingDetails {
		model.details.Focus()
	} else {
		model.subject.Focus()
	}
	result, err := tea.NewProgram(model).Run()
	if err != nil {
		return "", "", err
	}
	final := result.(reportInputModel)
	if !final.done {
		return "", "", ErrCancelled
	}
	return final.subject.Value(), final.details.Value(), nil
}

type reportConfirmModel struct {
	confirmed bool
	cancelled bool
}

func (m reportConfirmModel) Init() tea.Cmd { return nil }
func (m reportConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "Y":
			m.confirmed = true
			return m, tea.Quit
		case "n", "N", "enter", "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m reportConfirmModel) View() string {
	if m.confirmed || m.cancelled {
		return ""
	}
	return "Submit this report to Thunder support? [y/N]\n"
}
func ConfirmReport() (bool, error) {
	result, err := tea.NewProgram(reportConfirmModel{}).Run()
	if err != nil {
		return false, err
	}
	return result.(reportConfirmModel).confirmed, nil
}
