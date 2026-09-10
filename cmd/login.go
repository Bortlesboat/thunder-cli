package cmd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	htemplate "html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/tui"
	helpmenus "github.com/Thunder-Compute/thunder-cli/tui/help-menus"
	"github.com/Thunder-Compute/thunder-cli/utils"
)

const (
	authURL     = "https://console.thundercompute.com/login/app"
	callbackURL = "http://127.0.0.1"
)

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

type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

type authPageData struct {
	Error       string
	Theme       string
	State       string
	Acknowledge bool
}

type Config struct {
	APIURL       string    `json:"api_url,omitempty"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

const DefaultAPIURL = "https://api.thundercompute.com:8443"

func getAPIURL() string {
	if envURL := os.Getenv("TNR_API_URL"); envURL != "" {
		return envURL
	}
	return DefaultAPIURL
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
	loginCmd.Flags().StringVar(&loginToken, "token", "", "Authenticate directly with a token instead of opening browser")
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

func runLogin() error {
	config, err := LoadConfig()
	if err == nil && config.Token != "" {
		client := api.NewClient(config.Token, getAPIURL())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := client.ValidateToken(ctx)
		if err != nil {
			if JSONOutput {
				return fmt.Errorf("token validation failed: %w", err)
			}
			PrintWarningSimple("Already logged in.")
			return nil
		}
		renderLoginOutput("already_logged_in", "Already logged in", result, PrintWarningSimple)
		return nil
	}

	// Check environment variable as fallback if no token in config file
	if err != nil || config == nil || config.Token == "" {
		if envToken := os.Getenv("TNR_API_TOKEN"); envToken != "" {
			client := api.NewClient(envToken, getAPIURL())
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := client.ValidateToken(ctx)
			if err != nil {
				return fmt.Errorf("token validation failed: %w", err)
			}

			authResp := AuthResponse{
				Token: envToken,
			}
			if err := saveConfig(authResp); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}
			renderLoginOutput("logged_in", "Logged in", result, PrintSuccessSimple)
			return nil
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

		authResp := AuthResponse{
			Token: loginToken,
		}
		if err := saveConfig(authResp); err != nil {
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
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &loginSession{ctx: ctx}
	manual := useManualLogin()
	// Every existing OAuth and organization redirect preserves return_uri.
	returnURI := callbackURL + "/callback?app=cli&handoff=manual"
	var authChan <-chan AuthResponse
	var errChan <-chan error
	if !manual {
		port, successes, failures, cleanup, err := startCallbackServerWithContext(ctx, state, session.authenticate)
		if err != nil {
			manual = true
		} else {
			defer cleanup()
			authChan, errChan = successes, failures
			returnURI = fmt.Sprintf("%s:%d/callback?app=cli&ack=1", callbackURL, port)
		}
	}
	authURLWithParams := buildAuthURL(state, returnURI)

	model := tui.NewLoginModel(authURLWithParams, manual, func(input string) (string, error) {
		credentials, err := parseLoginToken(input, state)
		if err != nil {
			return "", err
		}
		if err := session.authenticate(credentials); err != nil {
			return "", err
		}
		return credentials.Token, nil
	}, cancel)
	p := tea.NewProgram(model)

	if !manual {
		go func() {
			timer := time.NewTimer(5 * time.Minute)
			defer timer.Stop()
			for {
				select {
				case authResp := <-authChan:
					tui.SendLoginSuccess(p, authResp.Token)
					return
				case err := <-errChan:
					tui.SendLoginError(p, err)
				case <-timer.C:
					tui.SendLoginError(p, fmt.Errorf("automatic sign-in is still waiting; paste the token from your browser to continue"))
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

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func buildAuthURL(state, returnURI string) string {
	params := url.Values{}
	params.Add("state", state)
	params.Add("return_uri", returnURI)
	return fmt.Sprintf("%s?%s", authURL, params.Encode())
}

func startCallbackServerWithContext(ctx context.Context, expectedState string, authenticate func(AuthResponse) error) (int, <-chan AuthResponse, <-chan error, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, nil, nil, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	authChan := make(chan AuthResponse, 1)
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
		if len(params["token"]) != 1 || params.Get("token") == "" {
			fail(http.StatusBadRequest, "no token received; finish signing in with your browser")
			return
		}
		authResp := AuthResponse{Token: params.Get("token"), RefreshToken: params.Get("refresh_token")}
		if err := authenticate(authResp); err != nil {
			fail(http.StatusUnauthorized, err.Error())
			return
		}

		_ = authSuccessTemplate.Execute(w, authPageData{
			Theme: theme, State: expectedState, Acknowledge: params.Get("ack") == "1",
		})
		select {
		case authChan <- authResp:
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

	return port, authChan, errChan, cleanup, nil
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

func saveConfig(authResp AuthResponse) error {
	configDir, err := utils.ThunderDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "cli_config.json")

	config := Config{
		Token:        authResp.Token,
		RefreshToken: authResp.RefreshToken,
	}

	if authResp.ExpiresIn > 0 {
		config.ExpiresAt = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0o600)
}

// .thunder.json config
type ProjectConfig struct {
	APIURL string `json:"api_url,omitempty"`
}

// load .thunder.json config
func loadProjectConfig() *ProjectConfig {
	data, err := os.ReadFile(".thunder.json")
	if err != nil {
		return nil
	}

	var config ProjectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}

	return &config
}

func LoadConfig() (*Config, error) {
	if envToken := os.Getenv("TNR_API_TOKEN"); envToken != "" {
		config := &Config{
			APIURL: DefaultAPIURL,
			Token:  envToken,
		}
		if projectConfig := loadProjectConfig(); projectConfig != nil && projectConfig.APIURL != "" {
			config.APIURL = projectConfig.APIURL
		}
		if envAPIURL := os.Getenv("TNR_API_URL"); envAPIURL != "" {
			config.APIURL = envAPIURL
		}
		return config, nil
	}

	configDir, err := utils.ThunderDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(configDir, "cli_config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.APIURL == "" {
		config.APIURL = DefaultAPIURL
	}

	if projectConfig := loadProjectConfig(); projectConfig != nil && projectConfig.APIURL != "" {
		config.APIURL = projectConfig.APIURL
	}

	if envAPIURL := os.Getenv("TNR_API_URL"); envAPIURL != "" {
		config.APIURL = envAPIURL
	}

	return &config, nil
}

// logoutCmd represents the logout command
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out from Thunder Compute",
	Long:  `Log out from Thunder Compute and remove saved authentication credentials.`,
	Args:  wrapArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLogout()
	},
}

func init() {
	logoutCmd.SetHelpFunc(wrapHelp(helpmenus.RenderLogoutHelp))

	rootCmd.AddCommand(logoutCmd)
}

func runLogout() error {
	envToken := os.Getenv("TNR_API_TOKEN")

	configDir, err := utils.ThunderDir()
	if err != nil {
		return fmt.Errorf("failed to resolve thunder directory: %w", err)
	}

	configPath := filepath.Join(configDir, "cli_config.json")
	configExists := true
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configExists = false
	}

	if !configExists && envToken == "" {
		if JSONOutput {
			printJSON(map[string]string{"status": "not_logged_in"})
			return nil
		}
		PrintWarningSimple("You are not logged in.")
		return nil
	} else if envToken != "" {
		if JSONOutput {
			printJSON(map[string]string{"status": "environment_token_active"})
			return nil
		}
		PrintWarningSimple("You are authenticated via TNR_API_TOKEN environment variable.")
		return nil
	}

	if err := os.Remove(configPath); err != nil {
		return fmt.Errorf("failed to remove config file: %w", err)
	}

	if JSONOutput {
		printJSON(map[string]string{"status": "logged_out"})
	} else {
		PrintSuccessSimple("Successfully logged out from Thunder Compute!")
	}
	return nil
}
