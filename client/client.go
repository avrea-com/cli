// Package client owns authenticated API transport. Only invoice downloads
// follow redirects, dropping credentials when leaving the original origin.
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
			reason = "ReadError"
		}
	}
	return &TransportError{Message: message, Reason: reason}
}

// Track transport reads separately from decompression. HTTP/2 stream resets and
// malformed chunks are retryable losses; corrupt compressed content is not.
type responseReader struct {
	io.Reader
	err error
}

func (r *responseReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil && err != io.EOF {
		r.err = err
	}
	return n, err
}

func responseReadFailure(ctx context.Context, body *responseReader, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if body.err != nil {
		return transportFailure(body.err, "could not read API response", true)
	}
	return errors.New(message)
}

type HTTPError struct {
	Status     int
	Path       string
	Detail     string
	RetryAfter string
	// Reauthentication holds X-Reauthentication-Required: set on a 403 that
	// asks the caller to sign in again before the operation.
	Reauthentication string
	Payload          map[string]any
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }

// New copies the HTTP client so composing a command cannot change another
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
	return c.exchange(ctx, method, path, body, headers, trace, nil)
}

type APIResponse struct {
	Protocol, Status string
	Code             int
	Headers          http.Header
	Body             []byte
}

func (c *Client) CallAPI(ctx context.Context, method, path string, body []byte, headers http.Header) (APIResponse, error) {
	var response APIResponse
	// Canonical keys keep a caller's "authorization" from sending a second credential.
	canonical := make(http.Header, len(headers))
	for key, values := range headers {
		for _, value := range values {
			canonical.Add(key, value)
		}
	}
	headers = canonical
	if len(headers.Values("Authorization")) == 0 && c.Config.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Config.Token)
	}
	var payload any
	if body != nil {
		payload = rawRequestContent{data: body, contentType: "application/json", noTransportRetry: true}
	}
	data, err := c.exchange(ctx, method, path, payload, headers, true, &response)
	response.Body = data
	return response, err
}

func (c *Client) exchange(ctx context.Context, method, path string, body any, headers http.Header, trace bool, response *APIResponse) ([]byte, error) {
	if err := config.ValidateHost(c.Config.Host); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("invalid API path")
	}
	var reader io.Reader
	contentType := "application/json"
	if content, ok := body.(rawRequestContent); ok {
		reader = bytes.NewReader(content.data)
		contentType = content.contentType
	} else if body != nil {
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
	// Avrea CI reruns and dispatches must reach the API at most once per
	// attempt; net/http otherwise replays a POST on a reused connection.
	if content, ok := body.(rawRequestContent); ok && content.noTransportRetry {
		req.GetBody = nil
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	// CallAPI callers may choose their own Content-Type and User-Agent. The
	// response decoder below only understands the encodings requested here.
	for key, value := range map[string]string{"Content-Type": contentType, "User-Agent": c.UserAgent} {
		if response == nil || req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
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
		traced := path
		if response != nil {
			// Arbitrary CallAPI query parameters may carry secrets.
			traced, _, _ = strings.Cut(path, "?")
		}
		_, _ = fmt.Fprintf(c.Trace, "  → %s %s [%s]\n", method, c.Config.Host+traced, status)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// net/http errors may contain request URLs. Keep session material out of errors.
		return nil, transportFailure(err, fmt.Sprintf("could not reach %s (connection failed or timed out)", c.Config.Host), false)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyReader := &responseReader{Reader: resp.Body}
	var source io.Reader = bodyReader
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "zstd":
		decoder, e := zstd.NewReader(bodyReader, zstd.WithDecoderMaxMemory(64<<20))
		if e != nil {
			return nil, responseReadFailure(ctx, bodyReader, "invalid compressed API response")
		}
		defer decoder.Close()
		source = decoder
	case "gzip":
		decoder, e := gzip.NewReader(bodyReader)
		if e != nil {
			return nil, responseReadFailure(ctx, bodyReader, "invalid compressed API response")
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
		return nil, responseReadFailure(ctx, bodyReader, "could not read API response")
	}
	if len(data) > limit {
		return nil, errors.New("API response is too large")
	}
	if response != nil {
		response.Protocol = resp.Proto
		response.Status = resp.Status
		response.Code = resp.StatusCode
		response.Headers = resp.Header
		if encoding != "" && encoding != "identity" {
			// The body is returned decoded, so wire framing headers no longer describe it.
			response.Headers = resp.Header.Clone()
			response.Headers.Del("Content-Encoding")
			response.Headers.Del("Content-Length")
		}
		return data, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload map[string]any
		_ = json.Unmarshal(data, &payload)
		detail := errorDetail(payload["detail"])
		return nil, &HTTPError{Path: path, Status: resp.StatusCode, Detail: detail, RetryAfter: resp.Header.Get("Retry-After"), Reauthentication: resp.Header.Get("X-Reauthentication-Required"), Payload: payload}
	}
	return data, nil
}

func decodeObject(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	value, err := DecodeJSON(data)
	if errors.Is(err, errTrailingJSON) {
		return nil, errors.New("unexpected API response: invalid JSON")
	}
	result, ok := value.(map[string]any)
	if err != nil || !ok {
		return nil, errors.New("unexpected API response: expected a JSON object")
	}
	return result, nil
}

var errTrailingJSON = errors.New("unexpected data after JSON value")

// DecodeJSON parses exactly one JSON value in a single pass, keeping numbers exact.
func DecodeJSON(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errTrailingJSON
	}
	return value, nil
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
