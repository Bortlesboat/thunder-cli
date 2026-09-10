package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginRetriesInvalidInputWithoutRestarting(t *testing.T) {
	var inputs []string
	model := NewLoginModel("https://example.com/login", true, func(input string) (string, error) {
		inputs = append(inputs, input)
		if input == "bad" {
			return "", fmt.Errorf("invalid token; try again")
		}
		return "accepted-token", nil
	})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = updated.(LoginModel)
	model.tokenInput.SetValue("bad")
	updated, validate := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(LoginModel)
	assert.Equal(t, LoginStateValidating, model.State())
	_, duplicate := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, duplicate)
	updated, quit := model.Update(validate())
	model = updated.(LoginModel)
	assert.Nil(t, quit)
	assert.Equal(t, LoginStateTokenInput, model.State())
	assert.Equal(t, "bad", model.tokenInput.Value())
	assert.Contains(t, model.View(), "invalid token")
	assert.False(t, model.quitting)

	longURL := "http://127.0.0.1:43123/callback?token=" + strings.Repeat("a", 600) + "&state=current"
	model.tokenInput.SetValue("")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(longURL), Paste: true})
	model = updated.(LoginModel)
	updated, validate = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(LoginModel)
	updated, quit = model.Update(validate())
	model = updated.(LoginModel)
	assert.Equal(t, []string{"bad", longURL}, inputs)
	assert.Equal(t, LoginStateSuccess, model.State())
	assert.Equal(t, "accepted-token", model.Token())
	require.NotNil(t, quit)
}

func TestLoginTimeoutKeepsManualEntryUsable(t *testing.T) {
	model := NewLoginModel("https://example.com/login", false, nil)
	updated, _ := model.Update(loginErrorMsg{Err: fmt.Errorf("paste the token from your browser")})
	model = updated.(LoginModel)
	assert.Equal(t, LoginStateTokenInput, model.State())
	assert.False(t, model.quitting)
	assert.Contains(t, model.View(), "paste the token from your browser")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("token")})
	assert.Equal(t, "token", updated.(LoginModel).tokenInput.Value())
}

func TestLoginCancellationIgnoresLateValidation(t *testing.T) {
	model := NewLoginModel("https://example.com/login", true, nil)
	model.state = LoginStateValidating
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(LoginModel)
	updated, _ = model.Update(loginValidatedMsg{Token: "too-late"})
	assert.Equal(t, LoginStateCancelled, updated.(LoginModel).State())
}
