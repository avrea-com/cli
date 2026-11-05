package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

const deviceInvalidResponse = "The server returned an invalid device-login response."

const deviceExpired = "The login code expired. Run avr auth device-login again."

var deviceAccessToken = lazyRegexp(`^avk_[A-Za-z0-9_-]{43}$`)

// NewDeviceLogin signs in without opening a local browser or callback listener.
func NewDeviceLogin(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var noQR bool
	cmd := &cobra.Command{Use: "device-login", Short: "Sign in from a remote machine using a browser on any device.", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&noQR, "no-qr", false, "Hide the terminal QR code; still display the sign-in link.")
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, store, api, err := d.load()
		if err != nil {
			return err
		}
		if err := store.ValidateMutation(); err != nil {
			return err
		}
		key, err := deviceLogin(cmd.Context(), d, api, !noQR)
		if err != nil {
			if cmd.Context().Err() != nil {
				return &ExitError{130, "\nAborted!"}
			}
			return fmt.Errorf("Login failed: %w", err) //nolint:staticcheck // Python diagnostic.
		}
		return completeLogin(cmd.Context(), d, cfg, store, api, key)
	})
	return cmd
}

func deviceSafeURL(value string, configured bool) (*url.URL, error) {
	u, err := url.Parse(value)
	bad := strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r < 32 || r == 127 })
	if err == nil {
		host := strings.ToLower(u.Hostname())
		loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
		bad = bad || u.Host == "" || host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || !loopback))
		if port := u.Port(); port != "" {
			n, e := strconv.Atoi(port)
			bad = bad || e != nil || n > 65535
		}
	}
	if err != nil || bad {
		if configured {
			return nil, errors.New("Invalid API host for device login. Check your host configuration.") //nolint:staticcheck // Match the Python CLI diagnostic.
		}
		return nil, errors.New("The server returned an unsafe device-login URL.") //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	return u, nil
}

func deviceOrigin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

type deviceResponse struct {
	status int
	header http.Header
	fields map[string]any
}

// Device authorization never sends existing credentials or follows redirects.
// Use a private client so the ordinary API client's timeout is unchanged.
type deviceClient struct {
	http      *http.Client
	userAgent string
}

func (c deviceClient) request(ctx context.Context, endpoint string, form url.Values) (deviceResponse, error) {
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return deviceResponse{}, errors.New(deviceInvalidResponse) //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	req.Header.Set("User-Agent", c.userAgent)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return deviceResponse{}, deviceTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return deviceResponse{}, deviceTransportError(err)
	}
	result := deviceResponse{status: resp.StatusCode, header: resp.Header}
	if len(data) <= 1<<20 {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if decoder.Decode(&result.fields) != nil {
			result.fields = nil
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			result.fields = nil
		}
	}
	return result, nil
}

func deviceFailure(r deviceResponse) error {
	if r.status == 404 || r.status == 405 || r.status == 501 {
		return errors.New("This server does not support device login yet. Use avr auth login instead.") //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	if r.status == 429 {
		return errors.New("Too many login attempts. Wait a minute before trying again.") //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	messages := map[string]string{
		"access_denied":          "Device access was denied. Start a new login if needed.",
		"expired_token":          deviceExpired,
		"invalid_grant":          "This login request is no longer usable. Run avr auth device-login again.",
		"invalid_client":         "This server does not accept this CLI for device login.",
		"invalid_scope":          "This server does not support the requested Avrea access.",
		"invalid_request":        "The server rejected the device-login request.",
		"unsupported_grant_type": "This server does not support device login.",
	}
	code, _ := r.fields["error"].(string)
	if message := messages[code]; message != "" {
		return errors.New(message) //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	return fmt.Errorf("Device login failed (HTTP %d). Try again.", r.status) //nolint:staticcheck // Python diagnostic.
}

func devicePositiveInt(value any, limit int64) (int64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil && i > 0 && (limit == 0 || i <= limit)
}

func deviceTransportReason(err error) string {
	var failure *client.TransportError
	if errors.As(err, &failure) {
		return failure.Reason
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "ReadTimeout"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "RemoteProtocolError"
	}
	return "ConnectError"
}

func deviceTransportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	message := "Could not reach Avrea while trying to sign in with device login. Try again shortly."
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		message = "Timed out while trying to sign in with device login. Try again shortly."
	}
	return &client.TransportError{Message: message, Reason: deviceTransportReason(err)}
}

func deviceLogin(ctx context.Context, d Dependencies, api *client.Client, showQR bool) (string, error) {
	base, err := deviceSafeURL(api.Config.Host, true)
	if err != nil {
		return "", err
	}
	h := *api.HTTP
	h.Timeout = 15 * time.Second
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := deviceClient{http: &h, userAgent: api.UserAgent}
	return deviceAuthorize(ctx, d, c, base, showQR)
}

func deviceAuthorize(ctx context.Context, d Dependencies, c deviceClient, base *url.URL, showQR bool) (string, error) {
	r, err := c.request(ctx, base.String()+"/.well-known/oauth-authorization-server", nil)
	if err != nil {
		return "", err
	}
	if r.status != 200 {
		return "", deviceFailure(r)
	}
	issuer, issuerOK := r.fields["issuer"].(string)
	device, deviceOK := r.fields["device_authorization_endpoint"].(string)
	token, tokenOK := r.fields["token_endpoint"].(string)
	if !issuerOK || !deviceOK || !tokenOK {
		return "", errors.New(deviceInvalidResponse) //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	if strings.TrimRight(issuer, "/") != strings.TrimRight(base.String(), "/") {
		return "", errors.New("The device-login issuer does not match the configured API host.") //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	for _, endpoint := range []string{device, token} {
		u, err := deviceSafeURL(endpoint, false)
		if err != nil {
			return "", err
		}
		if deviceOrigin(u) != deviceOrigin(base) {
			return "", errors.New("Device-login endpoints must use the configured API origin.") //nolint:staticcheck // Match the Python CLI diagnostic.
		}
	}
	started := d.Now()
	r, err = c.request(ctx, device, url.Values{"client_id": {"avr-cli"}, "scope": {"avrea"}})
	if err != nil {
		return "", err
	}
	if r.status != 200 {
		return "", deviceFailure(r)
	}
	secret, secretOK := r.fields["device_code"].(string)
	code, codeOK := r.fields["user_code"].(string)
	link, linkOK := r.fields["verification_uri"].(string)
	complete, completeOK := r.fields["verification_uri_complete"].(string)
	expires, expiresOK := devicePositiveInt(r.fields["expires_in"], 86400)
	interval, intervalOK := int64(5), true
	if value, present := r.fields["interval"]; present {
		interval, intervalOK = devicePositiveInt(value, 86400)
	}
	if !secretOK || secret == "" || !codeOK || len(code) == 0 || len(code) > 32 || strings.ContainsFunc(code, func(r rune) bool { return r < 32 || r > 126 }) || !linkOK || (!completeOK && r.fields["verification_uri_complete"] != nil) || !expiresOK || !intervalOK {
		return "", errors.New(deviceInvalidResponse) //nolint:staticcheck // Match the Python CLI diagnostic.
	}
	verification, err := deviceSafeURL(link, false)
	if err != nil {
		return "", err
	}
	link = verification.String()
	if complete != "" {
		u, err := deviceSafeURL(complete, false)
		if err != nil {
			return "", err
		}
		if deviceOrigin(u) != deviceOrigin(verification) {
			return "", errors.New("The device-login links must use the same console origin.") //nolint:staticcheck // Match the Python CLI diagnostic.
		}
		link = u.String()
	}
	if base.String() != config.DefaultHost {
		if _, err := fmt.Fprintf(d.Out, "API: %s\n", base); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprintf(d.Out, "Open this link in a browser on any device:\n\n  %s\n\n", link); err != nil {
		return "", err
	}
	if complete == "" {
		if _, err := fmt.Fprintf(d.Out, "Code: %s\n", code); err != nil {
			return "", err
		}
	}
	if showQR {
		if err := showDeviceQR(d, link); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprintf(d.Out, "\nWaiting for approval (expires in %d seconds). Press Ctrl+C to cancel.\n", expires); err != nil {
		return "", err
	}
	return pollDevice(ctx, d, c, token, secret, started.Add(time.Duration(expires)*time.Second), time.Duration(interval)*time.Second)
}

func pollDevice(ctx context.Context, d Dependencies, c deviceClient, endpoint, secret string, deadline time.Time, interval time.Duration) (string, error) {
	delay, lastError := interval, ""
	for {
		remaining := deadline.Sub(d.Now())
		if remaining > 0 {
			if err := d.Wait(ctx, min(delay, remaining)); err != nil {
				return "", err
			}
		}
		if !d.Now().Before(deadline) {
			if lastError != "" {
				return "", fmt.Errorf("Could not complete device login before expiry (%s). Check your connection and run avr auth device-login again.", lastError) //nolint:staticcheck // Match the Python CLI diagnostic.
			} //nolint:staticcheck // Python diagnostic.
			return "", errors.New(deviceExpired) //nolint:staticcheck // Match the Python CLI diagnostic.
		}
		r, err := c.request(ctx, endpoint, url.Values{"client_id": {"avr-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {secret}})
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			delay = min(delay, 24*time.Hour) * 2
			lastError = deviceTransportReason(err)
			if _, err := fmt.Fprintf(d.Err, "Warning: could not check device approval (%s); retrying.\n", lastError); err != nil {
				return "", err
			}
			continue
		}
		if r.status == 200 {
			key, _ := r.fields["access_token"].(string)
			kind, _ := r.fields["token_type"].(string)
			_, validExpiry := devicePositiveInt(r.fields["expires_in"], 0)
			if !deviceAccessToken().MatchString(key) || !strings.EqualFold(kind, "bearer") || r.fields["scope"] != "avrea" || !validExpiry {
				return "", errors.New(deviceInvalidResponse) //nolint:staticcheck // Match the Python CLI diagnostic.
			}
			return key, nil
		}
		if r.status == 404 || r.status == 405 || r.status == 501 {
			return "", deviceFailure(r)
		}
		retryAfter := deviceRetryAfter(r.header.Get("Retry-After"))
		if r.status == 408 || r.status >= 500 {
			lastError = fmt.Sprintf("HTTP %d", r.status)
			if _, err := fmt.Fprintf(d.Err, "Warning: could not check device approval (%s); retrying.\n", lastError); err != nil {
				return "", err
			}
			delay = max(interval*2, retryAfter)
			continue
		}
		code, _ := r.fields["error"].(string)
		if code == "slow_down" {
			interval += 5 * time.Second
		} else if code != "authorization_pending" && r.status != 429 {
			return "", deviceFailure(r)
		}
		lastError = ""
		delay = max(interval, retryAfter)
	}
}

func deviceRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	// Any larger delay also exhausts the maximum one-day authorization lifetime.
	if err != nil || n > 86400 {
		return 24 * time.Hour
	}
	return time.Duration(n) * time.Second
}
