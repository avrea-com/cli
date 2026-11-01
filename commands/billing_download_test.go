package commands

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

type brokenInvoiceBody struct{}

func (brokenInvoiceBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenInvoiceBody) Close() error             { return nil }

func TestInvoiceReadFailurePreservesDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "existing.pdf")
	if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: brokenInvoiceBody{}, Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"billing", "invoices", "download", "inv-019a0000000070008000000000000004", "--out", destination}, d)
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "keep" || code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "invoice PDF download failed") {
		t.Fatal(string(data), err, code, out, stderr)
	}
}

func TestInvoiceIDIsOneURLSegmentAndFilenameUsesBasename(t *testing.T) {
	t.Chdir(t.TempDir())
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.EscapedPath(), "/invoices/nested%2Finv-019a0000000070008000000000000004/pdf") {
			t.Fatal(r.URL.EscapedPath())
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("%PDF-fixture")), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"billing", "invoices", "download", "nested/inv-019a0000000070008000000000000004"}, d)
	data, err := os.ReadFile("inv-019a0000000070008000000000000004.pdf")
	if code != 0 || err != nil || string(data) != "%PDF-fixture" {
		t.Fatal(code, err, out, stderr)
	}
}
