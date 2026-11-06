package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

type samlSignalledReader struct {
	io.Reader
	started chan struct{}
}

func (r *samlSignalledReader) Read(p []byte) (int, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	return r.Reader.Read(p)
}
func TestSAMLConfigureCancellationStopsBlockedStdinBeforePOST(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	started := make(chan struct{}, 1)
	d.In = &samlSignalledReader{reader, started}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled upload reached API")
		return nil, errors.New("unexpected request")
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"org", "saml", "configure", "-"}, d) }()
	<-started
	cancel()
	if code := <-done; code != 130 {
		t.Fatal(code, stderr)
	}
}
func TestSAMLConfigurePreservesXMLWithoutPrintingIt(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	data := "<EntityDescriptor>private_fixture_xml\x00\xff</EntityDescriptor>"
	d.In = strings.NewReader(data)
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != data || r.Header.Get("Content-Type") != "application/xml" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Fatal(string(body), err, r.Header)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/orgs/"+inspectionOrg+"/saml" || r.URL.Query().Get("jit_provisioning") != "true" {
			t.Fatal(r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("{\"data\":{\"idp_entity_id\":\"fixture\"}}"))}, nil
	})}
	code := Execute(t.Context(), []string{"org", "saml", "configure", "-", "--verbose", "--json", "idp_entity_id"}, d)
	if code != 0 || calls != 1 || strings.Contains(out.String()+stderr.String(), "private_fixture_xml") {
		t.Fatal(code, calls, out, stderr)
	}
}
func TestSAMLBrowserFailureLeavesUsableURLWithoutLeakingCause(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.OpenBrowser = func(context.Context, string) error { return errors.New("private browser details") }
	err := openFlowBrowser(t.Context(), d, "https://api.example.test/saml/acme/test/login", "Opened browser.")
	if err != nil || out.Len() != 0 || !strings.Contains(stderr.String(), "Please open the URL manually.") || strings.Contains(stderr.String(), "private") {
		t.Fatal(err, out, stderr)
	}
}

func TestSAMLMetadataInputBound(t *testing.T) {
	data, err := readSAMLMetadata(t.Context(), strings.NewReader(strings.Repeat("x", (16<<20)+1)))
	if err == nil || data != nil || !strings.Contains(err.Error(), "input limit") {
		t.Fatal(len(data), err)
	}
}
