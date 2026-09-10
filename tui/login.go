package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Thunder-Compute/thunder-cli/tui/theme"
)

type LoginState int

const (
	LoginStateWaiting LoginState = iota
	LoginStateTokenInput
	LoginStateValidating
	LoginStateSuccess
	LoginStateCancelled
)

type LoginModel struct {
	state        LoginState
	authURL      string
	tokenInput   textinput.Model
	token        string
	err          error
	quitting     bool
	manual       bool
	authenticate func(string) (string, error)

	styles loginStyles
}

type loginSuccessMsg struct {
	Token string
}

type loginErrorMsg struct {
	Err error
}

type loginCancelMsg struct{}

type loginValidatedMsg struct {
	Token string
	Err   error
}

type loginStyles struct {
	prompt lipgloss.Style
	help   lipgloss.Style
	input  lipgloss.Style
}

func newLoginStyles() loginStyles {
	return loginStyles{
		prompt: SubtleTextStyle().MarginBottom(1),
		help:   HelpStyle().MarginTop(1),
		input: theme.Body().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1).
			MarginBottom(1),
	}
}

func NewLoginModel(authURL string, manual bool, authenticate func(string) (string, error)) LoginModel {
	styles := newLoginStyles()

	ti := textinput.New()
	ti.Placeholder = "Paste your token or callback URL..."
	ti.CharLimit = 0
	ti.EchoMode = textinput.EchoPassword
	ti.Width = 50
	ti.Focus()
	ti.PromptStyle = CursorStyle()
	ti.TextStyle = CursorStyle()
	ti.PlaceholderStyle = SubtleTextStyle()
	ti.Cursor.Style = CursorStyle()

	return LoginModel{
		state:        LoginStateWaiting,
		authURL:      authURL,
		tokenInput:   ti,
		styles:       styles,
		manual:       manual,
		authenticate: authenticate,
	}
}

func (m LoginModel) Init() tea.Cmd {
	var instructions strings.Builder
	instructions.WriteString(m.styles.prompt.Render("Open this link in your browser to sign in:"))
	instructions.WriteString("\n")
	instructions.WriteString(theme.Label().Underline(true).Render(m.authURL))
	instructions.WriteString("\n\n")
	if m.manual {
		instructions.WriteString("After signing in, copy the token shown in your browser.")
	} else {
		instructions.WriteString("Waiting for browser sign-in. If this terminal is still waiting, copy the token shown in your browser.")
	}
	instructions.WriteString("\n")
	instructions.WriteString(m.styles.help.Render("Press 'T' to paste a token or callback URL. Press 'Q' to cancel."))

	// Keep the waiting screen outside Bubble Tea's live renderer. The terminal
	// can then reflow every line on resize without leaving stale render rows,
	// while preserving the complete URL for copying and Ctrl-clicking.
	return tea.Println(instructions.String())
}

func (m LoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.quitting {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.state = LoginStateCancelled
			m.quitting = true
			return m, tea.Quit
		}
		switch m.state {
		case LoginStateWaiting:
			switch msg.String() {
			case "esc", "q", "Q", "ctrl+c":
				m.state = LoginStateCancelled
				m.quitting = true
				return m, tea.Quit
			case "t", "T":
				m.state = LoginStateTokenInput
				m.tokenInput.Focus()
				return m, tea.EnterAltScreen
			}
		case LoginStateTokenInput:
			switch msg.String() {
			case "esc":
				m.state = LoginStateWaiting
				m.tokenInput.Blur()
				return m, tea.ExitAltScreen
			case "enter":
				input := strings.TrimSpace(m.tokenInput.Value())
				if input != "" {
					m.state = LoginStateValidating
					m.err = nil
					return m, func() tea.Msg {
						token, err := m.authenticate(input)
						return loginValidatedMsg{Token: token, Err: err}
					}
				}
			default:
				m.tokenInput, cmd = m.tokenInput.Update(msg)
				return m, cmd
			}
		}

	case loginSuccessMsg:
		m.state = LoginStateSuccess
		m.token = msg.Token
		m.quitting = true
		return m, tea.Quit

	case loginErrorMsg:
		if m.state == LoginStateValidating {
			return m, nil
		}
		m.state = LoginStateTokenInput
		m.err = msg.Err
		m.tokenInput.Focus()
		return m, tea.EnterAltScreen

	case loginCancelMsg:
		m.state = LoginStateCancelled
		m.quitting = true
		return m, tea.Quit

	case loginValidatedMsg:
		if msg.Err != nil {
			m.state = LoginStateTokenInput
			m.err = msg.Err
			return m, nil
		}
		m.state = LoginStateSuccess
		m.token = msg.Token
		m.quitting = true
		return m, tea.Quit
	}

	return m, cmd
}

func (m LoginModel) View() string {
	if m.quitting || m.state == LoginStateWaiting {
		return ""
	}

	var b strings.Builder
	if m.state == LoginStateTokenInput || m.state == LoginStateValidating {
		b.WriteString(m.styles.prompt.Render("Paste your token or callback URL:"))
		b.WriteString("\n")
		b.WriteString(m.styles.input.Render(m.tokenInput.View()))
		if m.err != nil {
			b.WriteString("\n" + m.err.Error() + "\n")
		}
		if m.state == LoginStateValidating {
			b.WriteString(m.styles.help.Render("Verifying token... Press Ctrl+C to cancel."))
		} else {
			b.WriteString(m.styles.help.Render("Press Enter to submit or retry, 'Esc' to go back"))
		}
	}

	return b.String()
}

func (m LoginModel) State() LoginState {
	return m.state
}

func (m LoginModel) Token() string {
	if strings.TrimSpace(m.token) != "" {
		return m.token
	}
	return m.tokenInput.Value()
}

func (m LoginModel) Error() error {
	return m.err
}

func SendLoginSuccess(p *tea.Program, token string) {
	if p != nil {
		p.Send(loginSuccessMsg{Token: token})
	}
}

func SendLoginError(p *tea.Program, err error) {
	if p != nil {
		p.Send(loginErrorMsg{Err: err})
	}
}

func SendLoginCancel(p *tea.Program) {
	if p != nil {
		p.Send(loginCancelMsg{})
	}
}
