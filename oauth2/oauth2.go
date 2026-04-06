package oauth2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Grant types.
const (
	GrantClientCredentials = "client_credentials"
	GrantAuthorizationCode = "authorization_code"
	GrantDeviceCode        = "device_code"
)

// Params holds all OAuth2 directives parsed from a request block.
type Params struct {
	Grant        string
	TokenURL     string
	AuthURL      string   // authorization_code only
	DeviceURL    string   // device_code only
	ClientID     string
	ClientSecret string   // optional for public clients
	Scope        string
	RedirectPort string   // authorization_code: local callback port, default "9876"
	CacheFile    string   // optional token cache file
}

// DirectiveRegexes maps directive names to compiled regexes.
var DirectiveRegexes = map[string]*regexp.Regexp{
	"grant":         regexp.MustCompile(`^\s*#\s*@oauth2-grant\s*=\s*(.+?)\s*$`),
	"token-url":     regexp.MustCompile(`^\s*#\s*@oauth2-token-url\s*=\s*(.+?)\s*$`),
	"auth-url":      regexp.MustCompile(`^\s*#\s*@oauth2-auth-url\s*=\s*(.+?)\s*$`),
	"device-url":    regexp.MustCompile(`^\s*#\s*@oauth2-device-url\s*=\s*(.+?)\s*$`),
	"client-id":     regexp.MustCompile(`^\s*#\s*@oauth2-client-id\s*=\s*(.+?)\s*$`),
	"client-secret": regexp.MustCompile(`^\s*#\s*@oauth2-client-secret\s*=\s*(.+?)\s*$`),
	"scope":         regexp.MustCompile(`^\s*#\s*@oauth2-scope\s*=\s*(.+?)\s*$`),
	"redirect-port": regexp.MustCompile(`^\s*#\s*@oauth2-redirect-port\s*=\s*(.+?)\s*$`),
}

// ParseParams extracts OAuth2 directives from request block text lines.
// Returns nil if no @oauth2-grant directive is found.
func ParseParams(lines []string) *Params {
	p := &Params{}
	found := false
	for _, line := range lines {
		if m := DirectiveRegexes["grant"].FindStringSubmatch(line); m != nil {
			p.Grant = m[1]
			found = true
		}
		if m := DirectiveRegexes["token-url"].FindStringSubmatch(line); m != nil {
			p.TokenURL = m[1]
		}
		if m := DirectiveRegexes["auth-url"].FindStringSubmatch(line); m != nil {
			p.AuthURL = m[1]
		}
		if m := DirectiveRegexes["device-url"].FindStringSubmatch(line); m != nil {
			p.DeviceURL = m[1]
		}
		if m := DirectiveRegexes["client-id"].FindStringSubmatch(line); m != nil {
			p.ClientID = m[1]
		}
		if m := DirectiveRegexes["client-secret"].FindStringSubmatch(line); m != nil {
			p.ClientSecret = m[1]
		}
		if m := DirectiveRegexes["scope"].FindStringSubmatch(line); m != nil {
			p.Scope = m[1]
		}
		if m := DirectiveRegexes["redirect-port"].FindStringSubmatch(line); m != nil {
			p.RedirectPort = m[1]
		}
	}
	if !found {
		return nil
	}
	if p.RedirectPort == "" {
		p.RedirectPort = "9876"
	}
	return p
}

// tokenResponse is the standard OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// AcquireToken fetches a token using the configured grant type.
// It checks the cache first and stores the result after acquisition.
func AcquireToken(p *Params, cache *Cache) (string, error) {
	// Check cache
	if cache != nil {
		if tok := cache.Get(cacheKey(p)); tok != "" {
			return tok, nil
		}
	}

	var tok string
	var expiresIn int
	var err error

	switch p.Grant {
	case GrantClientCredentials:
		tok, expiresIn, err = clientCredentials(p)
	case GrantAuthorizationCode:
		tok, expiresIn, err = authorizationCode(p)
	case GrantDeviceCode:
		tok, expiresIn, err = deviceCode(p)
	default:
		return "", fmt.Errorf("unsupported oauth2 grant type: %q", p.Grant)
	}

	if err != nil {
		return "", err
	}

	if cache != nil && expiresIn > 0 {
		cache.Set(cacheKey(p), tok, expiresIn)
	}

	return tok, nil
}

func cacheKey(p *Params) string {
	return fmt.Sprintf("%s::%s::%s::%s", p.Grant, p.TokenURL, p.ClientID, p.Scope)
}

// ---------------------------------------------------------------------------
// Client Credentials
// ---------------------------------------------------------------------------

func clientCredentials(p *Params) (string, int, error) {
	vals := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {p.ClientID},
	}
	if p.ClientSecret != "" {
		vals.Set("client_secret", p.ClientSecret)
	}
	if p.Scope != "" {
		vals.Set("scope", p.Scope)
	}

	return postToken(p.TokenURL, vals)
}

// ---------------------------------------------------------------------------
// Authorization Code + PKCE
// ---------------------------------------------------------------------------

func authorizationCode(p *Params) (string, int, error) {
	// Generate PKCE verifier + challenge
	verifier, challenge, err := pkce()
	if err != nil {
		return "", 0, fmt.Errorf("pkce: %w", err)
	}

	state, err := randomString(16)
	if err != nil {
		return "", 0, err
	}

	redirectURI := fmt.Sprintf("http://localhost:%s/callback", p.RedirectPort)

	// Build auth URL
	authURL, err := url.Parse(p.AuthURL)
	if err != nil {
		return "", 0, fmt.Errorf("invalid auth URL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if p.Scope != "" {
		q.Set("scope", p.Scope)
	}
	authURL.RawQuery = q.Encode()

	// Start local callback server
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", p.RedirectPort))
	if err != nil {
		return "", 0, fmt.Errorf("could not bind to port %s: %w", p.RedirectPort, err)
	}

	srv := &http.Server{}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth2: state mismatch")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth2: missing code in callback")
			return
		}
		fmt.Fprintln(w, "<html><body><h2>Authorization successful — you can close this tab.</h2></body></html>")
		codeCh <- code
		go func() { _ = srv.Shutdown(context.Background()) }()
	})

	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
	}()

	fmt.Printf("\nOpening browser for OAuth2 authorization...\nIf browser doesn't open, visit:\n  %s\n\n", authURL.String())
	openBrowser(authURL.String())

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return "", 0, err
	case <-time.After(5 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return "", 0, fmt.Errorf("oauth2: timed out waiting for authorization")
	}

	// Exchange code for token
	vals := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {p.ClientID},
		"code_verifier": {verifier},
	}
	if p.ClientSecret != "" {
		vals.Set("client_secret", p.ClientSecret)
	}

	return postToken(p.TokenURL, vals)
}

// ---------------------------------------------------------------------------
// Device Code
// ---------------------------------------------------------------------------

func deviceCode(p *Params) (string, int, error) {
	// Step 1: request device + user code
	vals := url.Values{
		"client_id": {p.ClientID},
	}
	if p.Scope != "" {
		vals.Set("scope", p.Scope)
	}

	resp, err := http.PostForm(p.DeviceURL, vals)
	if err != nil {
		return "", 0, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &dc); err != nil {
		return "", 0, fmt.Errorf("device code parse: %w", err)
	}

	interval := dc.Interval
	if interval == 0 {
		interval = 5
	}

	fmt.Printf("\n== OAuth2 Device Authorization ==\nGo to: %s\nEnter code: %s\n\nWaiting for authorization...\n\n",
		dc.VerificationURI, dc.UserCode)

	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)

		pollVals := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dc.DeviceCode},
			"client_id":   {p.ClientID},
		}
		tok, exp, pollErr := postToken(p.TokenURL, pollVals)
		if pollErr != nil {
			// authorization_pending and slow_down are expected during polling
			if strings.Contains(pollErr.Error(), "authorization_pending") {
				continue
			}
			if strings.Contains(pollErr.Error(), "slow_down") {
				interval += 5
				continue
			}
			return "", 0, pollErr
		}
		return tok, exp, nil
	}

	return "", 0, fmt.Errorf("oauth2: device code expired")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func postToken(tokenURL string, vals url.Values) (string, int, error) {
	resp, err := http.PostForm(tokenURL, vals)
	if err != nil {
		return "", 0, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("token parse: %w", err)
	}
	if tr.Error != "" {
		return "", 0, fmt.Errorf("oauth2 error %q: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return "", 0, fmt.Errorf("oauth2: empty access_token in response")
	}

	tokenType := tr.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return tr.AccessToken, tr.ExpiresIn, nil
}

func pkce() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(rawURL string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", rawURL}
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	default:
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	_ = exec.Command(cmd, args...).Start()
}
