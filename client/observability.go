package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/coder/websocket"
)

// StreamError retains retry decisions without leaking transport URLs or headers.
type StreamError struct {
	HTTPStatus int
	CloseCode  int
	Reason     string
}

func (e *StreamError) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("HTTP %d", e.HTTPStatus)
	}
	return e.Reason
}

type streamResult struct {
	data []byte
	err  error
}

// ObservabilityStream has a single reader. Idle ticks don't cancel Read: doing
// that would close the WebSocket and reconnect every time the stream is quiet.
type ObservabilityStream struct {
	conn    *websocket.Conn
	cancel  context.CancelFunc
	results chan streamResult
	done    chan struct{}
	once    sync.Once
}

func (c *Client) OpenObservability(ctx context.Context, path, signal string, timeout time.Duration) (*ObservabilityStream, error) {
	if err := config.ValidateHost(c.Config.Host); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#") || (signal != "metrics" && signal != "status") {
		return nil, errors.New("invalid observability stream path or signal")
	}
	h := *c.HTTP
	h.Timeout = 0
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Match the REST client's direct connection policy without mutating a shared transport.
	transport := h.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if t, ok := transport.(*http.Transport); ok {
		direct := t.Clone()
		direct.Proxy = nil
		h.Transport = direct
	}
	defer h.CloseIdleConnections()
	headers := http.Header{"User-Agent": {c.UserAgent}}
	if c.Config.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Config.Token)
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	// Dial owns the response body, including failed handshakes (see its API contract).
	conn, response, err := websocket.Dial(dialCtx, strings.TrimRight(c.Config.Host, "/")+path+"?signals="+signal, &websocket.DialOptions{HTTPClient: &h, HTTPHeader: headers}) //nolint:bodyclose
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if response != nil && response.StatusCode != http.StatusSwitchingProtocols {
			return nil, &StreamError{HTTPStatus: response.StatusCode}
		}
		return nil, &StreamError{Reason: "connection failed or timed out"}
	}
	conn.SetReadLimit(16 << 20)
	readCtx, stop := context.WithCancel(ctx)
	s := &ObservabilityStream{conn: conn, cancel: stop, results: make(chan streamResult, 1), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			_, data, err := conn.Read(readCtx)
			if err != nil {
				data = nil
				if readCtx.Err() != nil {
					err = readCtx.Err()
				} else {
					failure := &StreamError{CloseCode: int(websocket.CloseStatus(err)), Reason: "connection closed"}
					var closed websocket.CloseError
					if errors.As(err, &closed) && closed.Reason != "" {
						failure.Reason = closed.Reason
					}
					err = failure
				}
			}
			select {
			case s.results <- streamResult{data, err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return s, nil
}

// Next returns nil data on an idle tick. The owner must Close even after error.
func (s *ObservabilityStream) Next(ctx context.Context, idle time.Duration) ([]byte, error) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-s.results:
		return result.data, result.err
	case <-timer.C:
		return nil, nil
	}
}

func (s *ObservabilityStream) Close() {
	s.once.Do(func() {
		s.cancel()
		_ = s.conn.CloseNow()
		<-s.done
	})
}
