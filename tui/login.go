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
	cancel       func()
	source       LoginCredentialSource

	styles loginStyles
}

type LoginCredentialSource int

const (
	LoginCredentialSourceUnknown LoginCredentialSource = iota
	LoginCredentialSourceBrowser
	LoginCredentialSourceManual
)

type LoginCompletion struct {
	Token  string
	Source LoginCredentialSource
}

type loginSuccessMsg struct {
	LoginCompletion
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

func NewLoginModel(authURL string, manual bool, authenticate func(string) (string, error), cancel func()) LoginModel {
	styles := newLoginStyles()

	ti := textinput.New()
	ti.Placeholder = "Paste your named API token..."
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
		cancel:       cancel,
	}
}

func (m LoginModel) Init() tea.Cmd {
	var instructions strings.Builder
	instructions.WriteString(m.styles.prompt.Render("Open this link in your browser to sign in:"))
	instructions.WriteString("\n")
	instructions.WriteString(theme.Label().Underline(true).Render(m.authURL))
	instructions.WriteString("\n\n")
	if m.manual {
		instructions.WriteString("Create a named API token in Console Settings, then paste it here.")
	} else {
		instructions.WriteString("Waiting for browser sign-in. You can also paste a named API token from Console Settings.")
	}
	instructions.WriteString("\n")
	instructions.WriteString(m.styles.help.Render("Press 'T' to paste a named API token. Press 'Q' to cancel."))

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
			return m.cancelLogin()
		}
		switch m.state {
		case LoginStateWaiting:
			switch msg.String() {
			case "esc", "q", "Q":
				return m.cancelLogin()
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
		m.source = msg.Source
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
		return m.cancelLogin()

	case loginValidatedMsg:
		if msg.Err != nil {
			m.state = LoginStateTokenInput
			m.err = msg.Err
			return m, nil
		}
		m.state = LoginStateSuccess
		m.token = msg.Token
		m.source = LoginCredentialSourceManual
		m.quitting = true
		return m, tea.Quit
	}

	return m, cmd
}

func (m LoginModel) cancelLogin() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	m.state = LoginStateCancelled
	m.quitting = true
	return m, tea.Quit
}

func (m LoginModel) View() string {
	if m.quitting || m.state == LoginStateWaiting {
		return ""
	}

	var b strings.Builder
	if m.state == LoginStateTokenInput || m.state == LoginStateValidating {
		b.WriteString(m.styles.prompt.Render("Paste your named API token:"))
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

func (m LoginModel) Completion() LoginCompletion {
	return LoginCompletion{
		Token:  m.Token(),
		Source: m.source,
	}
}

func (m LoginModel) Error() error {
	return m.err
}

func SendLoginSuccess(p *tea.Program, completion LoginCompletion) {
	if p != nil {
		p.Send(loginSuccessMsg{LoginCompletion: completion})
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
