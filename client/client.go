// Package client owns authenticated API transport. It never follows redirects.
package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/klauspost/compress/zstd"
)

type Client struct {
	Config    config.Config
	HTTP      *http.Client
	UserAgent string
	Trace     io.Writer
	Timings   io.Writer
}

// TransportError preserves retry classification without exposing net/http's
// original error, which can include sensitive request URLs.
type TransportError struct{ Message, Reason string }

func (e *TransportError) Error() string { return e.Message }

func transportFailure(err error, message string, reading bool) error {
	reason := "ConnectError"
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		reason = "ReadTimeout"
		var operation *net.OpError
		if errors.As(err, &operation) && operation.Op == "dial" {
			reason = "ConnectTimeout"
		}
	} else if reading {
		switch {
		case errors.Is(err, io.ErrUnexpectedEOF):
			reason = "RemoteProtocolError"
		case errors.As(err, &network):
			reason = "ReadError"
		default:
			return errors.New(message)
		}
	}
	return &TransportError{Message: message, Reason: reason}
}

type HTTPError struct {
	Status     int
	Path       string
	Detail     string
	RetryAfter string
	Payload    map[string]any
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }

// New copies the HTTP client so composing an admin command cannot change another
// client's redirect policy or timeout. Credentials are sent only to Config.Host.
func New(cfg config.Config, transport *http.Client, userAgent string) *Client {
	h := http.Client{Timeout: 30 * time.Second}
	if transport != nil {
		h = *transport
		if h.Timeout == 0 {
			h.Timeout = 30 * time.Second
		}
	}
	h.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{Config: cfg, HTTP: &h, UserAgent: userAgent}
}

func (c *Client) Do(ctx context.Context, method, path string, body any) (map[string]any, error) {
	data, err := c.Raw(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}

// Raw retains JSON member order for Python-compatible key/value output.
func (c *Client) Raw(ctx context.Context, method, path string, body any) ([]byte, error) {
	headers := make(http.Header)
	if c.Config.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Config.Token)
	}
	return c.request(ctx, method, path, body, headers, true)
}

// Request supports session-cookie requests during login. It does not inherit the
// API key: callers explicitly select the authentication for each request.
func (c *Client) Request(ctx context.Context, method, path string, body any, headers http.Header) (map[string]any, error) {
	data, err := c.request(ctx, method, path, body, headers, false)
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}

func (c *Client) request(ctx context.Context, method, path string, body any, headers http.Header, trace bool) ([]byte, error) {
	if err := config.ValidateHost(c.Config.Host); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("invalid API path")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Config.Host+path, reader)
	if err != nil {
		return nil, errors.New("could not construct API request")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept-Encoding", "zstd, gzip")
	var timing *requestTiming
	if c.Timings != nil {
		timing = newRequestTiming()
		req = req.WithContext(httptrace.WithClientTrace(ctx, timing.hooks()))
		defer timing.finish(c.Timings)
	}
	resp, err := c.HTTP.Do(req)
	if timing != nil {
		timing.response(resp)
	}
	if trace && c.Trace != nil {
		status := "connect error"
		if resp != nil {
			status = fmt.Sprint(resp.StatusCode)
		}
		_, _ = fmt.Fprintf(c.Trace, "  → %s %s [%s]\n", method, c.Config.Host+path, status)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// net/http errors may contain request URLs. Keep session material out of errors.
		return nil, transportFailure(err, fmt.Sprintf("could not reach %s (connection failed or timed out)", c.Config.Host), false)
	}
	defer func() { _ = resp.Body.Close() }()
	var source io.Reader = resp.Body
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "zstd":
		decoder, e := zstd.NewReader(resp.Body, zstd.WithDecoderMaxMemory(64<<20))
		if e != nil {
			return nil, errors.New("invalid compressed API response")
		}
		defer decoder.Close()
		source = decoder
	case "gzip":
		decoder, e := gzip.NewReader(resp.Body)
		if e != nil {
			return nil, errors.New("invalid compressed API response")
		}
		defer func() { _ = decoder.Close() }()
		source = decoder
	case "", "identity":
	default:
		return nil, errors.New("unsupported API response encoding")
	}
	// This client handles small JSON documents, never downloads or streaming logs.
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(source, limit+1))
	if timing != nil {
		timing.mark("body_read")
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, transportFailure(err, "could not read API response", true)
	}
	if len(data) > limit {
		return nil, errors.New("API response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload map[string]any
		_ = json.Unmarshal(data, &payload)
		detail := errorDetail(payload["detail"])
		return nil, &HTTPError{Path: path, Status: resp.StatusCode, Detail: detail, RetryAfter: resp.Header.Get("Retry-After"), Payload: payload}
	}
	return data, nil
}

func decodeObject(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var result map[string]any
	if d.Decode(&result) != nil || result == nil {
		return nil, errors.New("unexpected API response: expected a JSON object")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("unexpected API response: invalid JSON")
	}
	return result, nil
}

// FastAPI's field-level validation diagnostics are part of useful terminal output.
func errorDetail(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	var lines []string
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		loc, _ := fields["loc"].([]any)
		if len(loc) > 0 && (loc[0] == "body" || loc[0] == "query" || loc[0] == "path") {
			loc = loc[1:]
		}
		field := ""
		for _, part := range loc {
			if number, ok := part.(float64); ok {
				field += fmt.Sprintf("[%g]", number)
			} else {
				if field != "" {
					field += "."
				}
				field += fmt.Sprint(part)
			}
		}
		message := "invalid"
		if fields["msg"] != nil && fields["msg"] != "" {
			message = fmt.Sprint(fields["msg"])
		}
		if field != "" {
			message = field + ": " + message
		}
		lines = append(lines, message)
	}
	return strings.Join(lines, "; ")
}
