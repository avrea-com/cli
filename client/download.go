package client

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/klauspost/compress/zstd"
)

// DownloadError deliberately excludes URLs, credentials and transport internals.
type DownloadError struct{}

func (*DownloadError) Error() string {
	return "Artifact request failed: connection or stream interrupted"
}

type downloadBody struct {
	source       io.Reader
	body         io.Closer
	ctx          context.Context
	cancel       context.CancelFunc
	timer        *time.Timer
	idle         time.Duration
	finish       func()
	decoderClose func()
}

func (b *downloadBody) Read(p []byte) (int, error) {
	n, err := b.source.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF {
		if b.ctx.Err() != nil {
			return n, b.ctx.Err()
		}
		return n, &DownloadError{}
	}
	return n, err
}
func (b *downloadBody) Close() error {
	b.timer.Stop()
	b.cancel()
	if b.decoderClose != nil {
		b.decoderClose()
	}
	err := b.body.Close()
	b.finish()
	return err
}

// Download streams an authenticated response, with an inactivity timeout rather
// than a whole-file deadline. The caller must close the body. Redirects are
// refused even if the supplied transport allows them.
func (c *Client) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	return c.download(ctx, path, false)
}

// DownloadDecoded supports opaque analysis artifacts with the same compression
// negotiation as Python. The decoded bytes stream without the JSON size limit.
func (c *Client) DownloadDecoded(ctx context.Context, path string) (io.ReadCloser, error) {
	return c.download(ctx, path, true)
}

func (c *Client) download(ctx context.Context, path string, compressed bool) (io.ReadCloser, error) {
	if err := config.ValidateHost(c.Config.Host); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, errors.New("invalid API path")
	}
	child, cancel := context.WithCancel(ctx)
	idle := c.HTTP.Timeout
	if idle == 0 {
		idle = 30 * time.Second
	}
	timer := time.AfterFunc(idle, cancel)
	finish := func() {}
	fail := func(err error) (io.ReadCloser, error) { timer.Stop(); cancel(); finish(); return nil, err }
	req, err := http.NewRequestWithContext(child, http.MethodGet, c.Config.Host+path, nil)
	if err != nil {
		return fail(errors.New("could not construct API request"))
	}
	req.Header.Set("Accept-Encoding", "identity")
	if compressed {
		req.Header.Set("Accept-Encoding", "zstd, gzip")
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Config.Token)
	}
	var timing *requestTiming
	if c.Timings != nil {
		timing = newRequestTiming()
		req = req.WithContext(httptrace.WithClientTrace(child, timing.hooks()))
		finish = func() { timing.mark("body_read"); timing.finish(c.Timings) }
	}
	transport := *c.HTTP
	transport.Timeout = 0
	transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req) //nolint:bodyclose // Ownership transfers to downloadBody; errors close it below.
	if timing != nil {
		timing.response(resp)
	}
	if c.Trace != nil {
		status := "connect error"
		if resp != nil {
			status = fmt.Sprint(resp.StatusCode)
		}
		_, _ = fmt.Fprintf(c.Trace, "  → GET %s%s [%s]\n", c.Config.Host, path, status)
	}
	if err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(&DownloadError{})
	}
	timer.Reset(idle)
	body := &downloadBody{source: resp.Body, body: resp.Body, ctx: ctx, cancel: cancel, timer: timer, idle: idle, finish: finish}
	// Python requires identity so decompression cannot buffer an unbounded chunk
	// before the caller enforces the recorded artifact size.
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if !compressed && encoding != "" && encoding != "identity" {
		_ = body.Close()
		return nil, &DownloadError{}
	}
	if compressed {
		var decoded io.Reader
		var closeDecoder func()
		switch encoding {
		case "gzip":
			reader, err := gzip.NewReader(body)
			if err != nil {
				_ = body.Close()
				return nil, &DownloadError{}
			}
			decoded, closeDecoder = reader, func() { _ = reader.Close() }
		case "zstd":
			reader, err := zstd.NewReader(body, zstd.WithDecoderMaxMemory(64<<20))
			if err != nil {
				_ = body.Close()
				return nil, &DownloadError{}
			}
			decoded, closeDecoder = reader, reader.Close
		case "", "identity":
		default:
			_ = body.Close()
			return nil, &DownloadError{}
		}
		if decoded != nil {
			// The inner reader refreshes the idle timer on compressed wire progress;
			// the outer reader translates decoder failures and owns final cleanup.
			body = &downloadBody{source: decoded, body: resp.Body, decoderClose: closeDecoder, ctx: ctx, cancel: cancel, timer: timer, idle: idle, finish: finish}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = body.Close() }()
		const limit = 64 << 10
		data, readErr := io.ReadAll(io.LimitReader(body, limit+1))
		if readErr != nil {
			return nil, readErr
		}
		var payload map[string]any
		if len(data) <= limit {
			_ = json.Unmarshal(data, &payload)
		}
		return nil, &HTTPError{Path: path, Status: resp.StatusCode, Detail: errorDetail(payload["detail"]), RetryAfter: resp.Header.Get("Retry-After"), Payload: payload}
	}
	return body, nil
}
