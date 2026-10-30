package commands

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

const callbackURI = "http://localhost:8765/callback"

type callback struct{ session, csrf, sso string }

func NewLogin(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var provider, email string
	cmd := &cobra.Command{Use: "login", Short: "Authenticate via browser and store credentials.", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&provider, "provider", "github", "OAuth provider: github, google or gitlab.")
	cmd.Flags().StringVar(&email, "email", "", "Work email for company SSO discovery.")
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		provider = strings.ToLower(provider)
		if provider != "github" && provider != "google" && provider != "gitlab" {
			return &ExitError{2, "Error: --provider must be github, google or gitlab"}
		}
		cfg, store, api, err := d.load()
		if err != nil {
			return err
		}
		key, err := login(cmd.Context(), d, api, provider, email)
		if err != nil {
			return err
		}
		if err := store.Set(cfg.Host, "token", key); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
		api.Config.Token = key
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		user, _ := api.Do(ctx, http.MethodGet, "/users/me", nil)
		cancel()
		signedIn, _ := user["email"].(string)
		var b strings.Builder
		if signedIn != "" {
			fmt.Fprintf(&b, "%s Logged in as %s\n", d.paint("✓", "32"), d.paint(output.Text(signedIn), "1"))
		} else {
			b.WriteString(d.paint("✓", "32") + " Authentication complete.\n")
		}
		fmt.Fprintf(&b, "  Credentials saved to %s\n", store.Path)
		if store.Read().Value(cfg.Host, "default_org") == "" {
			ctx, cancel = context.WithTimeout(cmd.Context(), 5*time.Second)
			orgs, _ := api.Do(ctx, http.MethodGet, "/users/me/organizations", nil)
			cancel()
			data, _ := orgs["data"].([]any)
			if len(data) == 1 {
				org, _ := data[0].(map[string]any)
				id, _ := org["organization_id"].(string)
				slug, _ := org["slug"].(string)
				if slug == "" {
					slug = id
				}
				if id != "" {
					if err := store.Set(cfg.Host, "default_org", id); err != nil {
						return fmt.Errorf("save default organization: %w", err)
					}
					fmt.Fprintf(&b, "  Default organization: %s (%s)\n", d.paint(output.Text(slug), "1"), output.Text(id))
				}
			}
		}
		b.WriteString("\nTry: avr status\n")
		_, err = fmt.Fprint(d.Out, b.String())
		return err
	})
	return cmd
}

func login(parent context.Context, d Dependencies, api *client.Client, provider, email string) (string, error) {
	authURL := api.Config.Host + "/oauth/" + provider + "/login?next=" + callbackURI
	if provider == "google" {
		authURL = api.Config.Host + "/oauth/google/login?final_redirect=" + callbackURI
	}
	if email != "" {
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		discovery, err := api.Request(ctx, http.MethodPost, "/sso/discover", map[string]any{"email": email}, nil)
		cancel()
		if err != nil {
			return "", fmt.Errorf("login failed: could not check for SSO at %s: %w", api.Config.Host, err)
		}
		if discovery["method"] == "saml" {
			path, _ := discovery["saml_login_path"].(string)
			if path == "" {
				return "", fmt.Errorf("login failed: %s reported SSO for %s but returned no login path", api.Config.Host, output.Text(email))
			}
			// The API returns a relative login path, never an unrelated browser origin.
			parsed, err := url.Parse(path)
			if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" {
				return "", errors.New("login failed: invalid SSO login path")
			}
			authURL = api.Config.Host + path + "?" + url.Values{"next": {callbackURI}}.Encode()
		}
	}
	listener, err := d.Listen(parent, "tcp", "127.0.0.1:8765")
	if err != nil {
		return "", errors.New("cannot bind to port 8765. Ensure no other process is using this port and try again")
	}
	ctx, cancel := context.WithTimeout(parent, d.LoginTimeout)
	defer cancel()
	received := make(chan callback, 1)
	var once sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		once.Do(func() {
			q := r.URL.Query()
			cb := callback{session: q.Get("session"), csrf: q.Get("csrf_token"), sso: q.Get("sso_required")}
			if cb.session == "" {
				if cookie, e := r.Cookie("avrea_session"); e == nil {
					cb.session = cookie.Value
				}
			}
			label := "Authentication failed. Return to your terminal."
			if cb.session != "" {
				headers, e := sessionHeaders(callback{session: cb.session})
				if e == nil {
					lookup, stop := context.WithTimeout(ctx, 3*time.Second)
					user, _ := api.Request(lookup, http.MethodGet, "/users/me", nil, headers)
					stop()
					label = "CLI session connected. Return to your terminal."
					if email, ok := user["email"].(string); ok && email != "" {
						label = "Signed in as " + email + ". Return to your terminal."
					}
				}
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			_, _ = fmt.Fprintf(w, "<!doctype html><html><head><title>Avrea CLI</title></head><body><h1>Avrea CLI</h1><p>%s</p><p>You can close this tab now.</p></body></html>", html.EscapeString(label))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			received <- cb
		})
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, BaseContext: nil}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() { _ = server.Close(); <-serveErr }()
	if _, err := fmt.Fprintf(d.Out, "Opening browser to: %s\nWaiting for authentication...\n", authURL); err != nil {
		return "", err
	}
	// Browser invocations can wait for the callback, so serve concurrently.
	if err := d.OpenBrowser(ctx, authURL); err != nil {
		_, _ = fmt.Fprintln(d.Err, "Failed to open browser. Please visit:")
		if _, err := fmt.Fprintln(d.Out, authURL); err != nil {
			return "", err
		}
	}
	var cb callback
	select {
	case cb = <-received:
	case <-ctx.Done():
		if parent.Err() != nil {
			return "", parent.Err()
		}
		return "", errors.New("authentication timed out waiting for browser callback")
	}
	if cb.session == "" {
		if cb.sso != "" {
			return "", fmt.Errorf("%s requires single sign-on.\nRetry with your work email so the CLI can find your identity provider:\n  avr auth login --email you@yourcompany.com", output.Text(cb.sso))
		}
		return "", errors.New("Authentication failed - no session received") //nolint:staticcheck // Preserve the existing user-facing CLI diagnostic.
	}
	headers, err := sessionHeaders(cb)
	if err != nil {
		return "", err
	}
	name, err := d.Hostname()
	if err != nil {
		return "", errors.New("could not determine hostname for API key")
	}
	exchange, stop := context.WithTimeout(parent, 10*time.Second)
	result, err := api.Request(exchange, http.MethodPost, "/users/me/api-keys", map[string]any{"name": "CLI - " + name, "expires_days": 90}, headers)
	stop()
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) {
			return "", fmt.Errorf("%s returned HTTP %d when creating an API key", api.Config.Host, he.Status)
		}
		return "", fmt.Errorf("could not create an API key at %s", api.Config.Host)
	}
	key, ok := result["api_key"].(string)
	if !ok || strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") {
		return "", errors.New("invalid API key response")
	}
	logout, stop := context.WithTimeout(parent, 5*time.Second)
	_, _ = api.Request(logout, http.MethodPost, "/users/me/logout", nil, headers)
	stop()
	return key, nil
}

func sessionHeaders(cb callback) (http.Header, error) {
	cookie := &http.Cookie{Name: "avrea_session", Value: cb.session}
	if err := cookie.Valid(); err != nil {
		return nil, errors.New("invalid authentication callback")
	}
	if strings.ContainsAny(cb.csrf, "\r\n\x00") {
		return nil, errors.New("invalid authentication callback")
	}
	headers := make(http.Header)
	headers.Set("Cookie", cookie.String())
	if cb.csrf != "" {
		headers.Set("X-CSRF-Token", cb.csrf)
	}
	return headers, nil
}
