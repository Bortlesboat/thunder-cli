package cmd

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	htemplate "html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/internal/credentials"
	clitypes "github.com/Thunder-Compute/thunder-cli/pkg/types"
	"github.com/Thunder-Compute/thunder-cli/tui"
	helpmenus "github.com/Thunder-Compute/thunder-cli/tui/help-menus"
)

const callbackURL = "http://127.0.0.1"

const authSuccessHTML = `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Authentication Successful</title>
		<style>
			* {
				margin: 0;
				padding: 0;
				box-sizing: border-box;
			}

			html, body {
				font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji';
				color: #fafafa;
				height: 100vh;
				overflow: hidden;
				display: flex;
				flex-direction: column;
				align-items: center;
				justify-content: center;
				padding: 20px;
				text-rendering: optimizeLegibility;
				-webkit-font-smoothing: antialiased;
				-moz-osx-font-smoothing: grayscale;
			}

			body {
				background: #0a0a0a;
			}

			.logo-container {
				margin-bottom: 32px;
				display: flex;
				justify-content: center;
				align-items: center;
			}

			.logo-container svg {
				width: 120px;
				height: 120px;
			}

			h1 {
				font-size: 28px;
				font-weight: 700;
				color: #fafafa;
				margin-bottom: 12px;
				letter-spacing: -0.02em;
				text-align: center;
				display: flex;
				align-items: center;
				gap: 12px;
				justify-content: center;
			}

			.message {
				font-size: 16px;
				line-height: 1.6;
				color: #a3a3a3;
				margin-bottom: 24px;
				text-align: center;
				max-width: 400px;
			}

			body.light {
				background: #ffffff;
				color: #171717;
			}

			body.light h1 {
				color: #171717;
			}

			body.light .message {
				color: #525252;
			}

			body.light svg .s0 {
				fill: #171717;
			}
		</style>
	</head>
	<body class="{{.Theme}}">
		<div class="logo-container">
			<svg xmlns="http://www.w3.org/2000/svg" version="1.2" viewBox="0 0 970 970" width="970" height="970">
				<style>
					.s0 { fill: #95c5ea }
				</style>
				<path class="s0" d="m818.76 13.83l-537.43 314.98c-5.04 2.96-2.95 10.69 2.9 10.69h418.32c5.22 0 7.73 6.39 3.91 9.95l-216.05 200.92c-5.24 4.88-12.89-2.31-8.34-7.84l132.36-161.12c3.07-3.75 0.41-9.38-4.44-9.38h-400.82c-1.03 0-2.03 0.27-2.92 0.79l-135.41 79.71c-5.04 2.97-2.94 10.69 2.91 10.69h346.15c21.1 0 33.27 23.96 20.82 41l-324.53 444.17c-4.03 5.53 3.26 12.21 8.41 7.71l774.9-676.69c3.99-3.48 1.53-10.06-3.77-10.06h-200.72c-16.04 0-25.51-17.96-16.46-31.19l147.86-216.14c3.46-5.06-2.35-11.29-7.65-8.19z"/>
			</svg>
		</div>

		<h1>
			Authentication Successful!
		</h1>

		<p class="message">
			You can now close this window and return to your terminal.
		</p>
		{{if .Acknowledge}}
		<script>window.parent.postMessage({type: "thunder-auth-complete", state: {{.State}}}, "https://console.thundercompute.com");</script>
		{{end}}
	</body>
	</html>
`

const authFailedHTML = `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Authentication Failed</title>
		<style>
			* {
				margin: 0;
				padding: 0;
				box-sizing: border-box;
			}

			html, body {
				font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji';
				color: #fafafa;
				height: 100vh;
				overflow: hidden;
				display: flex;
				flex-direction: column;
				align-items: center;
				justify-content: center;
				padding: 20px;
				text-rendering: optimizeLegibility;
				-webkit-font-smoothing: antialiased;
				-moz-osx-font-smoothing: grayscale;
			}

			body {
				background: #0a0a0a;
			}

			.logo-container {
				margin-bottom: 32px;
				display: flex;
				justify-content: center;
				align-items: center;
			}

			.logo-container svg {
				width: 120px;
				height: 120px;
			}

			h1 {
				font-size: 28px;
				font-weight: 700;
				margin-bottom: 12px;
				letter-spacing: -0.02em;
				text-align: center;
			}

			.message {
				font-size: 16px;
				line-height: 1.6;
				color: #a3a3a3;
				margin-bottom: 24px;
				text-align: center;
				max-width: 400px;
			}

			.error {
				background: rgba(239, 68, 68, 0.1);
				border: 1px solid rgba(239, 68, 68, 0.3);
				border-radius: 8px;
				padding: 12px 16px;
				color: #fca5a5;
				margin-bottom: 16px;
				max-width: 400px;
				text-align: center;
				overflow-wrap: anywhere;
			}

			body.light {
				background: #ffffff;
				color: #171717;
			}

			body.light .message {
				color: #525252;
			}

			body.light .error {
				background: rgba(239, 68, 68, 0.08);
				border-color: #fecaca;
				color: #b91c1c;
			}

			body.light svg .s0 {
				fill: #171717;
			}
		</style>
	</head>
	<body class="{{.Theme}}">
		<div class="logo-container">
			<svg xmlns="http://www.w3.org/2000/svg" version="1.2" viewBox="0 0 970 970" width="970" height="970">
				<style>
					.s0 { fill: #95c5ea }
				</style>
				<path class="s0" d="m818.76 13.83l-537.43 314.98c-5.04 2.96-2.95 10.69 2.9 10.69h418.32c5.22 0 7.73 6.39 3.91 9.95l-216.05 200.92c-5.24 4.88-12.89-2.31-8.34-7.84l132.36-161.12c3.07-3.75 0.41-9.38-4.44-9.38h-400.82c-1.03 0-2.03 0.27-2.92 0.79l-135.41 79.71c-5.04 2.97-2.94 10.69 2.91 10.69h346.15c21.1 0 33.27 23.96 20.82 41l-324.53 444.17c-4.03 5.53 3.26 12.21 8.41 7.71l774.9-676.69c3.99-3.48 1.53-10.06-3.77-10.06h-200.72c-16.04 0-25.51-17.96-16.46-31.19l147.86-216.14c3.46-5.06-2.35-11.29-7.65-8.19z"/>
			</svg>
		</div>

		<h1>Authentication Failed</h1>
		<div class="error">Error: {{.Error}}</div>
		<p class="message">You can now close this window and return to your terminal.</p>
	</body>
	</html>
`

var (
	authSuccessTemplate = htemplate.Must(htemplate.New("success").Parse(authSuccessHTML))
	authFailedTemplate  = htemplate.Must(htemplate.New("failed").Parse(authFailedHTML))
)

type authPageData struct {
	Error       string
	Theme       string
	State       string
	Acknowledge bool
}

var loginToken string
var loginManual bool

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Thunder Compute",
	Args:  wrapArgs(cobra.NoArgs),
}

func init() {
	loginCmd.RunE = func(cmd *cobra.Command, args []string) error { return runLogin() }
	loginCmd.SetHelpFunc(wrapHelp(helpmenus.RenderLoginHelp))

	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().StringVar(&loginToken, "token", "", "Authenticate with a named API token instead of opening a browser")
	loginCmd.Flags().BoolVar(&loginManual, "manual", false, "Finish login by pasting a token (selected automatically over SSH)")
}

func loginMessage(prefix string, result *api.ValidateTokenResult) string {
	if result != nil && result.Email != "" {
		if result.OrgName != "" {
			return fmt.Sprintf("%s as %s (%s).", prefix, result.Email, result.OrgName)
		}
		return fmt.Sprintf("%s as %s.", prefix, result.Email)
	}
	return prefix + "."
}

type loginJSONOutput struct {
	Status       string `json:"status"`
	Email        string `json:"email,omitempty"`
	Organization string `json:"organization,omitempty"`
}

func renderLoginOutput(status, message string, result *api.ValidateTokenResult, humanOutput func(string)) {
	output := loginJSONOutput{Status: status}
	if result != nil {
		output.Email = result.Email
		output.Organization = result.OrgName
	}
	if JSONOutput {
		printJSON(output)
		return
	}
	humanOutput(loginMessage(message, result))
}

func savedCredentialRequiresLogin(err error) bool {
	if errors.Is(err, credentials.ErrReauthenticationRequired) {
		return true
	}
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}

func runLogin() error {
	config, err := LoadConfig()
	if err == nil && config.Token != "" {
		client, err := newAuthenticatedClient(config)
		var result *api.ValidateTokenResult
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			result, err = client.ValidateToken(ctx)
			cancel()
		}
		if err == nil {
			renderLoginOutput("already_logged_in", "Already logged in", result, PrintWarningSimple)
			return nil
		}
		if config.CredentialSource == CredentialSourceEnvironment {
			return fmt.Errorf("TNR_API_TOKEN could not be validated: %w", err)
		}
		if !savedCredentialRequiresLogin(err) {
			return fmt.Errorf("could not validate saved credentials: %w", err)
		}
		if JSONOutput && loginToken == "" {
			return fmt.Errorf("token validation failed: %w", err)
		}
		if !JSONOutput {
			PrintWarningSimple("Saved credentials are no longer valid; starting a new login.")
		}
	}

	if loginToken != "" {
		client := api.NewClient(loginToken, getAPIURL())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := client.ValidateToken(ctx)
		if err != nil {
			return fmt.Errorf("token validation failed: %w", err)
		}
		if result.CredentialType != clitypes.CredentialTypeAPIToken {
			return usageErr("--token accepts only named Thunder API tokens; create one in Console Settings")
		}

		authResp := AuthResponse{
			Token: loginToken,
		}
		if err := saveConfig(authResp, CredentialSourceProvidedToken, result); err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}
		renderLoginOutput("logged_in", "Logged in", result, PrintSuccessSimple)
		return nil
	}

	if JSONOutput {
		return usageErr("--token or TNR_API_TOKEN is required with --json; browser login is interactive")
	}

	return runInteractiveLogin()
}

func useManualLogin() bool {
	if loginCmd.Flags().Changed("manual") {
		return loginManual
	}
	if loginManual || os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" {
		return true
	}
	return runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

func runInteractiveLogin() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &loginSession{ctx: ctx}
	manual := useManualLogin()
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}
	configCtx, configCancel := context.WithTimeout(ctx, 10*time.Second)
	oauthConfig, err := credentials.FetchOAuthConfig(configCtx, getAPIURL())
	configCancel()
	if err != nil {
		return fmt.Errorf("failed to load CLI OAuth configuration: %w", err)
	}
	authorizationURL, err := url.Parse(oauthConfig.AuthorizationURL)
	if err != nil {
		return fmt.Errorf("failed to parse authorization URL: %w", err)
	}
	authURLWithParams := authorizationURL.ResolveReference(&url.URL{Path: "/settings/tokens"}).String()
	var callbackChan <-chan oauthCallback
	var errChan <-chan error
	if !manual {
		verifier, err := credentials.GenerateCodeVerifier()
		if err != nil {
			return fmt.Errorf("failed to generate PKCE verifier: %w", err)
		}
		var redirectURI string
		port, successes, failures, cleanup, err := startCallbackServerWithContext(ctx, state, func(code string) error {
			return session.authenticateBrowser(*oauthConfig, code, redirectURI, verifier)
		})
		if err != nil {
			manual = true
		} else {
			defer cleanup()
			callbackChan, errChan = successes, failures
			redirectURI = fmt.Sprintf("%s:%d/callback", callbackURL, port)
			authURLWithParams, err = credentials.AuthorizationURL(*oauthConfig, redirectURI, state, credentials.CodeChallenge(verifier))
			if err != nil {
				return fmt.Errorf("failed to build authorization URL: %w", err)
			}
		}
	}

	model := tui.NewLoginModel(authURLWithParams, manual, func(input string) (string, error) {
		credential, err := parseLoginToken(input, state)
		if err != nil {
			return "", err
		}
		if err := session.authenticate(credential); err != nil {
			return "", err
		}
		return credential.Token, nil
	}, cancel)
	p := tea.NewProgram(model)
	if !manual {
		go func() {
			timer := time.NewTimer(5 * time.Minute)
			defer timer.Stop()
			for {
				select {
				case <-callbackChan:
					tui.SendLoginSuccess(p, tui.LoginCompletion{Source: tui.LoginCredentialSourceBrowser})
					return
				case err := <-errChan:
					tui.SendLoginError(p, err)
				case <-timer.C:
					tui.SendLoginError(p, fmt.Errorf("authentication timed out; paste a named API token from Console Settings to continue"))
				case <-ctx.Done():
					return
				}
			}
		}()
		if err := openBrowser(authURLWithParams); err != nil {
			fmt.Println("Open the link below in your browser to sign in.")
		}
	}

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("login interface error: %w", err)
	}
	if fm, ok := finalModel.(tui.LoginModel); ok {
		model = fm
	}
	if model.State() == tui.LoginStateSuccess {
		renderLoginOutput("logged_in", "Logged in", session.result, PrintSuccessSimple)
		return nil
	}
	if model.State() == tui.LoginStateCancelled {
		PrintWarningSimple("User cancelled authentication")
		return nil
	}
	return usageErr("authentication failed")
}

func completeInteractiveLogin(ctx context.Context, loginAPIURL string, oauthConfig credentials.OAuthConfig, completion tui.LoginCompletion, oauthTokens <-chan *credentials.TokenResponse) (*api.ValidateTokenResult, error) {
	token := completion.Token
	client := api.NewClient(token, loginAPIURL)

	result, err := client.ValidateToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch completion.Source {
	case tui.LoginCredentialSourceBrowser:
		var response *credentials.TokenResponse
		select {
		case response = <-oauthTokens:
		default:
			return nil, fmt.Errorf("browser login completed without OAuth credentials")
		}
		if result.CredentialType != clitypes.CredentialTypeCLIOAuth {
			return nil, fmt.Errorf("unexpected browser credential type %q returned by Thunder API", result.CredentialType)
		}
		if err := saveBrowserOAuthConfig(ctx, loginAPIURL, oauthConfig, response); err != nil {
			return nil, fmt.Errorf("failed to save credentials: %w", err)
		}
	case tui.LoginCredentialSourceManual:
		if result.CredentialType != clitypes.CredentialTypeAPIToken {
			return nil, usageErr("manual login accepts only named Thunder API tokens; create one in Console Settings")
		}
		if err := saveConfig(AuthResponse{Token: token}, CredentialSourceProvidedToken, result); err != nil {
			return nil, fmt.Errorf("failed to save credentials: %w", err)
		}
	default:
		return nil, fmt.Errorf("login completed without credential source")
	}

	return result, nil
}

func saveBrowserOAuthConfig(ctx context.Context, loginAPIURL string, oauthConfig credentials.OAuthConfig, response *credentials.TokenResponse) error {
	expiresAt := response.ReceivedAt.Add(time.Duration(response.ExpiresIn) * time.Second)
	config := Config{
		OAuthConfigOrigin: loginAPIURL,
		OAuthConfig:       &oauthConfig,
		Token:             response.AccessToken,
		RefreshToken:      response.RefreshToken,
		ExpiresAt:         &expiresAt,
		CredentialSource:  CredentialSourceBrowser,
		CredentialType:    clitypes.CredentialTypeCLIOAuth,
	}
	if loginAPIURL != DefaultAPIURL {
		config.APIURL = loginAPIURL
	}
	return credentials.Save(ctx, config)
}

func generateState() (string, error) {
	return credentials.GenerateState()
}

type oauthCallback struct {
	Code string
}

func startCallbackServerWithContext(ctx context.Context, expectedState string, authenticate func(string) error) (int, <-chan oauthCallback, <-chan error, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, nil, nil, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	callbackChan := make(chan oauthCallback, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
	}

	reportError := func(err error) {
		select {
		case errChan <- err:
		default:
		}
	}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		theme := callbackTheme(r)
		fail := func(code int, message string) {
			reportError(fmt.Errorf("%s", message))
			w.WriteHeader(code)
			_ = authFailedTemplate.Execute(w, authPageData{Error: message, Theme: theme})
		}
		params := r.URL.Query()
		if len(params["state"]) != 1 || subtle.ConstantTimeCompare([]byte(params.Get("state")), []byte(expectedState)) != 1 {
			fail(http.StatusBadRequest, "invalid state parameter; use the link from the current login")
			return
		}
		if params.Get("error") != "" {
			fail(http.StatusUnauthorized, "browser authentication did not complete; open the login link and try again")
			return
		}
		if len(params["code"]) != 1 || params.Get("code") == "" {
			fail(http.StatusBadRequest, "no authorization code received; finish signing in with your browser")
			return
		}
		code := params.Get("code")
		if err := authenticate(code); err != nil {
			fail(http.StatusUnauthorized, err.Error())
			return
		}

		_ = authSuccessTemplate.Execute(w, authPageData{
			Theme: theme, State: expectedState, Acknowledge: params.Get("ack") == "1",
		})
		select {
		case callbackChan <- oauthCallback{Code: code}:
		case <-ctx.Done():
		default:
		}
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			reportError(err)
		}
	}()

	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = server.Shutdown(shutdownCtx) //nolint:errcheck // shutdown error is non-fatal
	}

	go func() {
		<-ctx.Done()
		cleanup()
	}()

	return port, callbackChan, errChan, cleanup, nil
}

func callbackTheme(r *http.Request) string {
	if r.URL.Query().Get("theme") == "light" {
		return "light"
	}
	return "dark"
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return usageErr("unsupported platform")
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// logoutCmd represents the logout command
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out from Thunder Compute",
	Long:  `Log out locally from Thunder Compute, optionally revoking this member's Thunder CLI access first.`,
	Args:  wrapArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLogout(logoutRevoke)
	},
}

var logoutRevoke bool

func init() {
	logoutCmd.SetHelpFunc(wrapHelp(helpmenus.RenderLogoutHelp))

	rootCmd.AddCommand(logoutCmd)
	logoutCmd.Flags().BoolVar(&logoutRevoke, "revoke", false, "Revoke this member's Thunder CLI access before removing local credentials")
}

const logoutRevokeTimeout = 10 * time.Second

type logoutJSONOutput struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type logoutError struct {
	status  string
	message string
	cause   error
}

func (e *logoutError) Error() string {
	if e.cause == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.cause)
}

func (e *logoutError) Unwrap() error    { return e.cause }
func (e *logoutError) UserFacing() bool { return true }
func (e *logoutError) JSONErrorOutput() any {
	return logoutJSONOutput{Status: e.status, Error: e.Error()}
}

func newLogoutError(status, message string, cause error) error {
	return &logoutError{status: status, message: message, cause: cause}
}

func renderLogoutSuccess(status, message string) {
	if JSONOutput {
		printJSON(logoutJSONOutput{Status: status})
		return
	}
	PrintSuccessSimple(message)
}

func runLogout(revoke bool) error {
	envToken := os.Getenv("TNR_API_TOKEN")

	configPath, err := credentials.Path()
	if err != nil {
		return fmt.Errorf("failed to resolve credential file: %w", err)
	}

	configExists := true
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configExists = false
	}

	if !configExists && envToken == "" {
		if JSONOutput {
			printJSON(logoutJSONOutput{Status: "not_logged_in"})
			return nil
		}
		PrintWarningSimple("You are not logged in.")
		return nil
	} else if envToken != "" {
		if revoke {
			return newLogoutError(
				"credential_not_revocable",
				"TNR_API_TOKEN is the active credential and cannot be revoked by tnr; unset the environment variable instead. Any saved credentials were kept",
				nil,
			)
		}
		if JSONOutput {
			printJSON(logoutJSONOutput{Status: "environment_token_active"})
			return nil
		}
		PrintWarningSimple("You are authenticated via TNR_API_TOKEN. Unset the environment variable to log out; any saved credentials were kept.")
		return nil
	}

	if revoke {
		return revokeAndRemoveConfig()
	}

	ctx, cancel := context.WithTimeout(context.Background(), logoutRevokeTimeout)
	defer cancel()
	if err := credentials.Delete(ctx); err != nil {
		return fmt.Errorf("failed to remove config file: %w", err)
	}

	renderLogoutSuccess("logged_out", "Successfully logged out locally from Thunder Compute!")
	return nil
}

func revokeAndRemoveConfig() error {
	config, err := LoadConfig()
	if err != nil {
		return newLogoutError("credential_not_revocable", "could not read saved credentials; the config file was kept", err)
	}

	if err := validateRevocableConfig(config); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), logoutRevokeTimeout)
	defer cancel()

	manager, err := credentials.NewManager(config)
	if err != nil {
		return newLogoutError("revocation_failed", "could not initialize CLI credential revocation; local credentials were kept", err)
	}
	if config.OAuthConfigOrigin == "" {
		return newLogoutError("revocation_failed", "saved CLI OAuth login is missing its trusted API origin; local credentials were kept", credentials.ErrReauthenticationRequired)
	}
	client := api.NewClientWithTokenSource(manager, config.OAuthConfigOrigin)
	if err := client.RevokeCLIOAuthGrant(ctx); err != nil {
		guidance := "Retry 'tnr logout --revoke', or use 'tnr logout' for local-only logout"
		var apiErr *api.APIError
		if errors.Is(err, credentials.ErrReauthenticationRequired) || (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized) {
			guidance = "This saved login cannot authenticate, which does not confirm that this member's CLI access was revoked. Use 'tnr logout' for local-only logout. To check or revoke member-wide CLI access, sign in to Console Settings > Connected apps"
		}
		return newLogoutError(
			"revocation_failed",
			"Thunder CLI access revocation could not be confirmed; local credentials were kept. "+guidance,
			err,
		)
	}
	if err := manager.Delete(ctx); err != nil {
		return newLogoutError(
			"local_logout_failed",
			"Thunder CLI access was revoked, but the local credential file could not be removed",
			err,
		)
	}

	renderLogoutSuccess("revoked_and_logged_out", "Thunder CLI access revoked for this member and local credentials removed. Other organization members and your Thunder Console session were not affected.")
	return nil
}

func validateRevocableConfig(config *Config) error {
	if config.CredentialSource != CredentialSourceBrowser {
		switch config.CredentialSource {
		case CredentialSourceProvidedToken:
			if config.CredentialType == clitypes.CredentialTypeAPIToken {
				return newLogoutError("credential_not_revocable", "provided API tokens are not revoked by tnr; delete the named token in Console Settings to revoke it everywhere. Local credentials were kept", nil)
			}
			return newLogoutError("credential_not_revocable", "manually supplied credentials are not revoked by tnr; local credentials were kept", nil)
		default:
			return newLogoutError("credential_not_revocable", "legacy credentials cannot be safely revoked remotely; use 'tnr logout', then log in through the browser. Console remains the place to revoke the legacy browser session. Local credentials were kept", nil)
		}
	}

	if config.CredentialType != clitypes.CredentialTypeCLIOAuth {
		if config.CredentialType == clitypes.CredentialTypeAPIToken {
			return newLogoutError("credential_not_revocable", "API tokens are not revoked by tnr; delete the named token in Console Settings to revoke it everywhere. Local credentials were kept", nil)
		}
		if config.CredentialType == clitypes.CredentialTypeStytchSession {
			return newLogoutError("credential_not_revocable", "legacy browser sessions are not revoked by tnr; use 'tnr logout', then log in again with the new CLI OAuth flow. Local credentials were kept", nil)
		}
		return newLogoutError("credential_not_revocable", "this saved credential is not a revocable Thunder CLI OAuth login; local credentials were kept", nil)
	}
	return nil
}
