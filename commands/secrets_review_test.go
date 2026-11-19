package commands

import (
	"context"
	"encoding/json"
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/spf13/cobra"
)

func TestConfigurationReviewInput(t *testing.T) {
	for _, tc := range []struct {
		name, input        string
		file, terminal, ok bool
	}{
		{"maximum with newline", strings.Repeat("x", configurationValueLimit) + "\r\n", false, false, true},
		{"truncation after newline", strings.Repeat("x", configurationValueLimit) + "\n\nx", false, false, false},
		{"file exact limit", strings.Repeat("x", configurationValueLimit), true, false, true},
		{"file newline exceeds limit", strings.Repeat("x", configurationValueLimit) + "\n", true, false, false},
		{"file terminal", "header\nbody\n", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _ := testDeps(t)
			d.In = strings.NewReader(tc.input)
			d.IsInputTerminal = func() bool { return tc.terminal }
			c := &cobra.Command{}
			c.SetContext(t.Context())
			value, err := configurationInputValue(c, d, "secret", "", tc.file)
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v error=%v", err == nil, err)
			}
			if tc.ok {
				want := tc.input
				if !tc.file {
					want = strings.TrimRight(want, "\r\n")
				}
				if value != want {
					t.Fatal("input changed")
				}
			}
			if tc.terminal && (err == nil || !strings.Contains(err.Error(), "file")) {
				t.Fatalf("missing file input guidance: %v", err)
			}
		})
	}
}
func TestConfigurationReviewDotenvWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.env")
	if err := os.WriteFile(path, []byte("TOKEN='first  \n second \n'\nOTHER= unquoted  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := configurationEnvFile(path)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	if entries[0].value != "first  \n second \n" || entries[1].value != "unquoted" {
		t.Fatalf("%q", entries)
	}
}
func TestConfigurationReviewMutationNeedsScope(t *testing.T) {
	for _, args := range [][]string{{"secret", "set", "TOKEN"}, {"secret", "edit", "TOKEN", "--label", "new"}, {"secret", "delete", "TOKEN", "--yes"}} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{})}
		code, _, stderr := configurationTest(t, s, args, "replacement")
		if code != 2 || !strings.Contains(stderr, "--org") {
			t.Fatal(args, code, stderr)
		}
		for _, call := range s.calls {
			if call.method != "GET" {
				t.Fatal("unexpected mutation", call)
			}
		}
	}
}
func TestConfigurationReviewSaveReceipt(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{Repositories: []string{testSecretRepo}, Environments: []string{"production"}})}
	code, out, stderr := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo}, "replacement")
	if code != 0 || !strings.Contains(out, "production") || !strings.Contains(out, s.values[0].Variants[0].ID()) || !strings.Contains(out, testSecretRepo) {
		t.Fatal(code, out, stderr)
	}
}
func TestConfigurationReviewWriteExitCodes(t *testing.T) {
	for _, tc := range []struct{ http, exit int }{{401, 4}, {422, 2}} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{}), failStage: "/value", code: tc.http}
		code, out, stderr := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg, "--json", "*"}, "replacement")
		if code != tc.exit || !strings.Contains(out, `"status": "failed"`) || strings.Contains(stderr, "not confirmed") {
			t.Fatal(code, out, stderr)
		}
	}
}
func TestConfigurationReviewEditRejected(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{}), failStage: "/" + testSecretID, code: 422}
	code, out, stderr := configurationTest(t, s, []string{"secret", "edit", "TOKEN", "--org", testSecretOrg, "--description", "new", "--json", "*"}, "")
	if code != 2 || !strings.Contains(out, `"status": "failed"`) {
		t.Fatal(code, out, stderr)
	}
}
func TestConfigurationReviewPatterns(t *testing.T) {
	for _, flag := range []string{"--env", "--branch", "--workflow"} {
		for _, pattern := range []string{"test?", "a+b", "[ab]", "!main", `a\b`} {
			s := &configurationServer{}
			code, _, stderr := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo, flag, pattern}, "replacement")
			if code != 2 || len(s.calls) != 0 || !strings.Contains(stderr, flag) {
				t.Fatal(flag, pattern, code, stderr, s.calls)
			}
		}
	}
}
func TestConfigurationReviewInputCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		defer func() { _ = reader.Close() }()
		defer func() { _ = writer.Close() }()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c := &cobra.Command{}
		c.SetContext(ctx)
		d, _, _ := testDeps(t)
		d = d.defaults()
		d.In = reader
		done := make(chan error, 1)
		go func() { _, err := configurationInputValue(c, d, "secret", "", false); done <- err }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancelled input accepted")
			}
		default:
			t.Fatal("input blocked after cancellation")
		}
	})
}

func TestConfigurationReviewVariableJSON(t *testing.T) {
	for _, value := range []*string{nil, ptr(""), ptr("configured")} {
		for _, verb := range []string{"list", "get"} {
			s := &configurationServer{values: []client.ConfigurationValue{{VariableID: "var-019a0000000070008000000000000001", Name: "TOKEN", Variants: []client.ScopedValue{{VariableVariantID: "vvr-019a0000000070008000000000000001", Value: value}}}}}
			args := []string{"variable", verb}
			if verb == "get" {
				args = append(args, "TOKEN")
			}
			args = append(args, "--json", "*", "--org", testSecretOrg)
			code, out, stderr := configurationTest(t, s, args, "")
			if code != 0 {
				t.Fatal(code, stderr)
			}
			var record map[string]any
			if verb == "list" {
				var rows []map[string]any
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatal(err)
				}
				record = rows[0]
				if _, ok := record["as_file"]; ok {
					t.Fatal(record)
				}
				record = record["variants"].([]any)[0].(map[string]any)
			} else if err := json.Unmarshal([]byte(out), &record); err != nil {
				t.Fatal(err)
			}
			if record["has_value"] != (value != nil) {
				t.Fatal(record)
			}
			for _, field := range []string{"secret_id", "variant_id", "rotation_reminder_days"} {
				if _, ok := record[field]; ok {
					t.Fatal(field, record)
				}
			}
		}
	}
}
func TestConfigurationReviewReceiptSurvivesFilterFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
		}
		d.In = strings.NewReader("secret-input")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := &configurationServer{values: sampleConfiguration(client.Availability{}), pending: cancelled}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { return s.roundTrip(t, r) })}
		d.Wait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
		expression := `error("output failure")`
		if cancelled {
			expression = ".version_id"
		}
		code := Execute(ctx, []string{"secret", "set", "TOKEN", "--org", testSecretOrg, "--json", "*", "--jq", expression}, d)
		if code == 0 || !strings.Contains(stderr.String(), testVersionID) || !strings.Contains(stderr.String(), "Retained write receipts") || strings.Contains(out.String()+stderr.String(), "secret-input") {
			t.Fatal(code, out, stderr)
		}
		puts := 0
		for _, call := range s.calls {
			if call.method == "PUT" {
				puts++
			}
		}
		if puts != 1 {
			t.Fatal(s.calls)
		}
	}
}
func TestConfigurationReviewImportResolvesRepositoriesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.env")
	if err := os.WriteFile(path, []byte("ONE=one\nTWO=two\nTHREE=three\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
	}
	lookups := 0
	s := &configurationServer{values: []client.ConfigurationValue{}}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/repos") {
			lookups++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"repository_id":"` + testSecretRepo + `","full_name":"acme/web"}],"pagination":{"next_cursor":null}}`)), Request: r}, nil
		}
		return s.roundTrip(t, r)
	})}
	code := Execute(t.Context(), []string{"secret", "set", "--env-file", path, "--repos", "web", "--org", testSecretOrg}, d)
	if code != 0 || lookups != 1 || len(s.calls) != 10 {
		t.Fatal(code, lookups, s.calls, out, stderr)
	}
}
func TestConfigurationReviewAuthenticationAndRateLimit(t *testing.T) {
	cfg := config.Config{Host: "https://api.example.test", Token: "fixture"}
	for _, tc := range []struct {
		status                      int
		reauth, detail, retry, want string
	}{
		{403, "required", "", "", "AVR_TOKEN"}, {403, "", "Organization is suspended", "", "suspended"}, {429, "", "", "17", "Retry in 17s"},
	} {
		err := configurationAPIError(cfg, &client.HTTPError{Status: tc.status, Reauthentication: tc.reauth, Detail: tc.detail, RetryAfter: tc.retry})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(err)
		}
	}
}

func TestConfigurationReviewPersonRequiredBeforeLookup(t *testing.T) {
	for _, token := range []string{"avs_fixture", "avt_fixture"} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: token, Org: "acme"}, nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("token should be rejected before any lookup")
			return nil, nil
		})}
		code := Execute(t.Context(), []string{"secret", "list"}, d)
		if code != 4 || !strings.Contains(stderr.String(), "unset AVR_TOKEN") {
			t.Fatal(code, out, stderr)
		}
	}
}
func TestConfigurationReviewExistingFileRejectsTerminal(t *testing.T) {
	for _, existing := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
		}
		d.IsInputTerminal = func() bool { return true }
		d.In = strings.NewReader("header\nbody\n")
		s := &configurationServer{values: []client.ConfigurationValue{}}
		args := []string{"secret", "set", "TOKEN", "--org", testSecretOrg}
		if existing {
			s.values = sampleConfiguration(client.Availability{})
			s.values[0].AsFile = true
		} else {
			args = append(args, "--type", "file", "--visibility", "all")
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { return s.roundTrip(t, r) })}
		code := Execute(t.Context(), args, d)
		if code != 2 || !strings.Contains(stderr.String(), "piped stdin") || len(s.calls) != 1 {
			t.Fatal(code, s.calls, out, stderr)
		}
	}
}

type configurationScopeWriter struct{}

func (configurationScopeWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "  Scope:") {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func TestConfigurationReviewScopeOutputFailureRetainsReceipt(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
	}
	d.In = strings.NewReader("secret-input")
	d.Out = configurationScopeWriter{}
	s := &configurationServer{values: sampleConfiguration(client.Availability{})}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { return s.roundTrip(t, r) })}
	code := Execute(t.Context(), []string{"secret", "set", "TOKEN", "--org", testSecretOrg}, d)
	if code == 0 || !strings.Contains(stderr.String(), testVersionID) || strings.Contains(stderr.String(), "secret-input") {
		t.Fatal(code, stderr)
	}
}
