package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestConfigSetPrefersExactIDAndPreservesOtherHostData(t *testing.T) {
	d, out, stderr := testDeps(t)
	cfg := config.Config{Host: "https://api.example.test", Token: "fixture"}
	d.LoadConfig = func() (config.Config, error) { return cfg, nil }
	raw := map[string]any{"default_host": "https://other.test", "hosts": map[string]any{
		cfg.Host:             map[string]any{"token": "saved", "future": map[string]any{"keep": true}},
		"https://other.test": map[string]any{"token": "other", "default_org": inspectionOrg},
	}}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(d.Store.Path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		body := "{\"data\":[{\"organization_id\":\"org-019a0000000070008000000000000009\",\"slug\":\"" + inspectionOrg + "\",\"name\":\"Wrong\"},{\"organization_id\":\"" + inspectionOrg + "\",\"slug\":\"acme\",\"name\":\"Acme\"}]}"
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"config", "set", "org", inspectionOrg}, d)
	stored := d.Store.Read()
	if code != 0 || stored.DefaultHost != "https://other.test" || stored.Value(cfg.Host, "default_org") != inspectionOrg || stored.Value(cfg.Host, "token") != "saved" || !strings.Contains(out.String(), "Acme") {
		t.Fatal(code, stored, out, stderr)
	}
	entry := stored.Entries[cfg.Host].(map[string]any)
	if entry["future"].(map[string]any)["keep"] != true {
		t.Fatal(stored)
	}
}
func TestConfigCancellationDoesNotSaveOrganization(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture"}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { cancel(); return nil, ctx.Err() })}
	code := Execute(ctx, []string{"config", "set", "org", inspectionOrg}, d)
	if code != 130 {
		t.Fatal(code, stderr)
	}
	if _, err := os.Stat(d.Store.Path); !os.IsNotExist(err) {
		t.Fatal("configuration changed", err)
	}
}
func TestConfigStatusDoesNotRevealTokenOrFetchScopedIdentity(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avs_secret_fixture", Org: inspectionOrg}, nil
	}
	d.GitRemote = func(context.Context) (string, error) { return "", nil }
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("scoped identity lookup"); return nil, nil })}
	code := Execute(t.Context(), []string{"config"}, d)
	if code != 0 || strings.Contains(out.String()+stderr.String(), "avs_secret_fixture") || !strings.Contains(out.String(), "not checked against the API") {
		t.Fatal(code, out, stderr)
	}
}
func TestConfigUnsetPreservesEmptyHostAndDoesNotCreateAbsentStore(t *testing.T) {
	store := &config.Store{Path: filepath.Join(t.TempDir(), "hosts.json")}
	if changed, err := store.ClearDefaultOrg("https://api.test"); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := store.Set("https://api.test", "default_org", inspectionOrg); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.ClearDefaultOrg("https://api.test"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	h := store.Read()
	if len(h.Order) != 1 || len(h.Entries["https://api.test"].(map[string]any)) != 0 || h.DefaultHost != "" {
		t.Fatal(h)
	}
}
