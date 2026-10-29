package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

func TestArtifactCancellationPreservesFile(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "artifact.zip")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"state": "complete", "size_bytes": json.Number("100"), "digest_sha256": strings.Repeat("0", 64), "file_name": "artifact.zip"}
	api := client.New(config.Config{Host: server.URL}, server.Client(), "test")
	_, err := downloadArtifact(ctx, api, "/download", row, destination)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "existing" {
		t.Fatal(string(data), err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}

func TestArtifactDownloadLargerThanJSONLimit(t *testing.T) {
	content := strings.Repeat("artifact", (17<<20)/8)
	sum := sha256.Sum256([]byte(content))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, content) }))
	defer server.Close()
	row := map[string]any{"state": "complete", "size_bytes": json.Number("17825792"), "digest_sha256": hex.EncodeToString(sum[:]), "file_name": "large.zip"}
	api := client.New(config.Config{Host: server.URL}, server.Client(), "test")
	destination, err := downloadArtifact(t.Context(), api, "/download", row, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Size() != int64(len(content)) {
		t.Fatal(info, err)
	}
}

func TestArtifactGetPreservesOrderAndExactNumbers(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg, Repo: "rep-019a0000000070008000000000000002"}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"data\":{\"z\":9007199254740993,\"a\":{\"last\":null,\"first\":true}}}")), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"artifact", "get", "art-019a0000000070008000000000000001"}, d)
	if code != 0 || out.String() != "z  9007199254740993\na  {'last': None, 'first': True}\n" {
		t.Fatal(code, out, stderr)
	}
}
