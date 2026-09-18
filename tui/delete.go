package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/utils"
)

type deleteStep int

const (
	deleteStepSelect deleteStep = iota
	deleteStepConfirm
	deleteStepComplete
)

type deleteModel struct {
	step      deleteStep
	cursor    int
	instances []api.Instance
	selected  *api.Instance
	confirmed bool
	quitting  bool
	client    *api.Client
	loading   bool
	spinner   spinner.Model
	err       error

	styles     PanelStyles
	warningBox lipgloss.Style
}

func NewDeleteModel(client *api.Client, instances []api.Instance) deleteModel {
	s := NewPrimarySpinner()

	ps := NewPanelStyles()
	// Override title with margins matching delete layout
	ps.Title = TitleStyle().MarginTop(1).MarginBottom(1)

	return deleteModel{
		step:       deleteStepSelect,
		client:     client,
		loading:    false,
		spinner:    s,
		instances:  instances,
		styles:     ps,
		warningBox: WarningBoxStyle().MarginTop(1).MarginBottom(1),
	}
}

func (m deleteModel) Init() tea.Cmd {
	return nil
}

func (m deleteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// Don't process keys while loading
		if m.loading {
			switch msg.String() {
			case "q", "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "esc":
			if m.step == deleteStepConfirm {
				m.step = deleteStepSelect
				m.cursor = 0
			} else {
				m.quitting = true
				return m, tea.Quit
			}

		case "enter":
			return m.handleEnter()

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			maxCursor := m.getMaxCursor()
			if m.cursor < maxCursor {
				m.cursor++
			}
		}
	}

	return m, nil
}

func (m deleteModel) handleEnter() (tea.Model, tea.Cmd) {
	switch m.step {
	case deleteStepSelect:
		if m.cursor < len(m.instances) {
			m.selected = &m.instances[m.cursor]
			m.step = deleteStepConfirm
			m.cursor = 0
		}

	case deleteStepConfirm:
		if m.cursor == 0 {
			m.confirmed = true
			m.step = deleteStepComplete
			return m, tea.Quit
		}
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m deleteModel) getMaxCursor() int {
	switch m.step {
	case deleteStepSelect:
		return len(m.instances) - 1
	case deleteStepConfirm:
		return 1 // Yes/No options
	}
	return 0
}

func (m deleteModel) View() string {
	if m.err != nil {
		return errorStyleTUI.Render(fmt.Sprintf("✗ Error: %v\n", m.err))
	}

	if m.quitting {
		return ""
	}

	if m.step == deleteStepComplete {
		return ""
	}

	var s strings.Builder

	s.WriteString(m.styles.Title.Render("⚡ Delete Thunder Compute Instance"))
	s.WriteString("\n")

	switch m.step {
	case deleteStepSelect:
		s.WriteString("Select an instance to delete:\n\n")

		for i, instance := range m.instances {
			cursor := "  "
			if m.cursor == i {
				cursor = m.styles.Cursor.Render("▶ ")
			}

			// Determine status style
			var statusStyle lipgloss.Style
			statusSuffix := ""
			switch instance.Status {
			case "RUNNING":
				statusStyle = SuccessStyle()
			case "STARTING":
				statusStyle = WarningStyle()
			case "DELETING":
				statusStyle = ErrorStyle()
				statusSuffix = " (already deleting)"
			default:
				statusStyle = lipgloss.NewStyle()
			}

			idAndName := fmt.Sprintf("(%s) %s", instance.ID, instance.Name)
			if m.cursor == i {
				idAndName = m.styles.Selected.Render(idAndName)
			}

			statusText := statusStyle.Render(fmt.Sprintf("(%s)", instance.Status))
			rest := fmt.Sprintf(" %s%s - %sx%s",
				statusText,
				statusSuffix,
				instance.NumGPUs,
				utils.FormatGPUType(instance.GPUType),
			)

			s.WriteString(fmt.Sprintf("%s%s%s\n", cursor, idAndName, rest))
		}

		s.WriteString("\n")
		s.WriteString(m.styles.Help.Render("↑/↓: Navigate  Enter: Select  Esc/Q: Quit\n"))

	case deleteStepConfirm:
		warning := "WARNING: This action is IRREVERSIBLE!\n\n" +
			"Deleting this instance will:\n" +
			"• Permanently destroy the instance and ALL data\n" +
			"• Remove all SSH configuration for this instance\n" +
			"• This action CANNOT be undone"
		s.WriteString(m.warningBox.Render(warning))
		s.WriteString("\n\n")

		var instanceInfo strings.Builder
		instanceInfo.WriteString(m.styles.Label.Render("ID:           ") + m.selected.ID + "\n")
		instanceInfo.WriteString(m.styles.Label.Render("Name:         ") + m.selected.Name + "\n")
		instanceInfo.WriteString(m.styles.Label.Render("Status:       ") + m.selected.Status + "\n")
		instanceInfo.WriteString(m.styles.Label.Render("GPU:          ") + m.selected.NumGPUs + "x" + utils.FormatGPUType(m.selected.GPUType) + "\n")
		instanceInfo.WriteString(m.styles.Label.Render("Template:     ") + utils.Capitalize(m.selected.Template))

		s.WriteString(m.styles.Panel.Render(instanceInfo.String()))
		s.WriteString("\n\n")

		s.WriteString("Are you sure you want to delete this instance?\n\n")

		options := []string{"✓ Yes, Delete Instance", "✗ No, Cancel"}
		for i, option := range options {
			cursor := "  "
			if m.cursor == i {
				cursor = m.styles.Cursor.Render("▶ ")
			}
			if i == 0 {
				s.WriteString(fmt.Sprintf("%s%s\n", cursor, ErrorStyle().Render(option)))
			} else {
				s.WriteString(fmt.Sprintf("%s%s\n", cursor, option))
			}
		}

		s.WriteString("\n")
		s.WriteString(m.styles.Help.Render("↑/↓: Navigate  Enter: Confirm  Esc: Back  Q: Quit\n"))
	}

	return s.String()
}

func RunDeleteInteractive(client *api.Client, instances []api.Instance) (*api.Instance, error) {
	InitCommonStyles(os.Stdout)
	m := NewDeleteModel(client, instances)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("error running TUI: %w", err)
	}

	result, ok := finalModel.(deleteModel)
	if !ok {
		return nil, fmt.Errorf("unexpected model type")
	}

	if result.err != nil {
		return nil, result.err
	}

	if result.quitting {
		return nil, ErrCancelled
	}

	if !result.confirmed || result.selected == nil {
		return nil, ErrCancelled
	}

	return result.selected, nil
}

func RunDeleteProgress(client *api.Client, instanceID string) (string, error) {
	err := RunProgress(fmt.Sprintf("Deleting instance %s...", instanceID), func() error {
		_, deleteErr := client.DeleteInstance(instanceID)
		return deleteErr
	}, nil)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully deleted Thunder Compute instance %s", instanceID), nil
}
