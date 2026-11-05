package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

type failingResponse struct{}

func (failingResponse) Read([]byte) (int, error) {
	return 0, errors.New("stream reset containing secret-url")
}
func (failingResponse) Close() error { return nil }

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResponseReadLossIsRetryableButCorruptContentIsNot(t *testing.T) {
	for _, encoding := range []string{"", "gzip", "zstd"} {
		for _, network := range []bool{false, true} {
			t.Run(encoding+"/"+fmt.Sprint(network), func(t *testing.T) {
				body := io.NopCloser(strings.NewReader("corrupt content"))
				if network {
					body = failingResponse{}
				}
				c := New(config.Config{Host: "https://api.example.test"}, &http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": {encoding}}, Body: body, Request: r}, nil
				})}, "test")
				_, err := c.Get(t.Context(), "/test", nil)
				var transport *TransportError
				if err == nil || errors.As(err, &transport) != network || strings.Contains(err.Error(), "secret-url") {
					t.Fatal(err)
				}
			})
		}
	}
}
