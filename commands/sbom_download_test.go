package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/klauspost/compress/zstd"
)

func TestSBOMDownloadStreamsLargeCompressedInventory(t *testing.T) {
	content := strings.Repeat("inventory", 2<<20)
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := encoder.EncodeAll([]byte(content), nil)
	_ = encoder.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("Accept-Encoding") != "zstd, gzip" {
			t.Error("missing transport contract")
		}
		w.Header().Set("Content-Encoding", "zstd")
		_, _ = w.Write(compressed)
	}))
	defer server.Close()
	d, out, stderr := testDeps(t)
	d.HTTP = server.Client()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "fixture", Org: inspectionOrg}, nil
	}
	target := filepath.Join(t.TempDir(), "inventory.json")
	code := Execute(t.Context(), []string{"sbom", "download", strings.Repeat("a", 40), "--repo", "rep-019a0000000070008000000000000002", "--format", "inventory", "--out", target}, d)
	data, err := os.ReadFile(target)
	if code != 0 || err != nil || string(data) != content {
		t.Fatal(code, len(data), err, out, stderr)
	}
}

func TestSBOMDownloadCancellationPreservesDestinationAndRemovesSpool(t *testing.T) {
	spoolDir := t.TempDir()
	t.Setenv("TMPDIR", spoolDir)
	t.Setenv("TMP", spoolDir)
	t.Setenv("TEMP", spoolDir)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	d, out, stderr := testDeps(t)
	d.HTTP = server.Client()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "fixture", Org: inspectionOrg}, nil
	}
	target := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	code := Execute(ctx, []string{"sbom", "download", strings.Repeat("a", 40), "--repo", "rep-019a0000000070008000000000000002", "--out", target}, d)
	data, err := os.ReadFile(target)
	if code != 130 || err != nil || string(data) != "old" || out.Len() != 0 {
		t.Fatal(code, string(data), err, out, stderr)
	}
	entries, err := os.ReadDir(spoolDir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestSBOMDownloadStdoutRetainsOpaqueBytes(t *testing.T) {
	content := []byte{0, 255, '\n', 'x'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) }))
	defer server.Close()
	d, out, stderr := testDeps(t)
	d.HTTP = server.Client()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"sbom", "download", strings.Repeat("a", 40), "--repo", "rep-019a0000000070008000000000000002", "--out", "-"}, d)
	if code != 0 || out.String() != string(content) || stderr.Len() != 0 {
		t.Fatal(code, out, stderr)
	}
}
