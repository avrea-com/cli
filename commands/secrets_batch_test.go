package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

func TestConfigurationVersionReadStates(t *testing.T) {
	for _, state := range []string{"ENABLED", "DISABLED", "DESTROYED"} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprint(state, jsonOutput), func(t *testing.T) {
				s := &configurationServer{values: sampleConfiguration(client.Availability{}), versionReply: &client.ValueWrite{VersionID: testVersionID, Status: state}}
				args := []string{"secret", "get", "TOKEN", "--org", testSecretOrg, "--version-id", testVersionID}
				if jsonOutput {
					args = append(args, "--json", "*")
				}
				code, out, errout := configurationTest(t, s, args, "")
				if code != 0 || !strings.Contains(out, testVersionID) || !strings.Contains(out, state) || errout != "" {
					t.Fatal(code, out, errout)
				}
			})
		}
	}
}
func TestConfigurationVersionReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name, id, status string
		http, exit       int
	}{
		{"missing", testVersionID, "", 404, 1}, {"server", testVersionID, "", 503, 1},
		{"session", testVersionID, "", 401, 4},
		{"wrong version", "svv-other", "ENABLED", 200, 1}, {"missing state", testVersionID, "", 200, 1},
		{"unknown state", testVersionID, "PENDING", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &configurationServer{values: sampleConfiguration(client.Availability{}), versionReply: &client.ValueWrite{VersionID: tc.id, Status: tc.status}, versionCode: tc.http}
			code, out, errout := configurationTest(t, s, []string{"secret", "get", "TOKEN", "--org", testSecretOrg, "--version-id", testVersionID, "--json", "*"}, "")
			if code != tc.exit || out != "" || errout == "" || strings.Contains(errout, "unconfirmed") || strings.Contains(errout, "submitted write") {
				t.Fatal(code, out, errout)
			}
		})
	}
}
func TestConfigurationPendingWriteTerminalStates(t *testing.T) {
	for _, state := range []string{"DISABLED", "DESTROYED"} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{}), pending: true, versionReply: &client.ValueWrite{VersionID: testVersionID, Status: state}}
		code, out, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg}, "replacement")
		if code != 1 || s.checks != 1 || len(s.calls) != 3 || !strings.Contains(out+errout, testVersionID) || !strings.Contains(errout, state) {
			t.Fatal(code, out, errout, s.calls)
		}
	}
}
func TestConfiguration503WriteOutcomes(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, stage := range []string{"name", "scope", "value", "edit scope", "edit description"} {
			t.Run(kind+stage, func(t *testing.T) {
				s := &configurationServer{values: sampleConfiguration(client.Availability{}), code: 503}
				args := []string{kind, "set", "TOKEN", "--org", testSecretOrg, "--json", "*"}
				switch stage {
				case "name":
					s.values = nil
					s.failStage = "/" + kind + "s"
					args = append(args, "--visibility", "all")
				case "scope":
					s.values[0].Variants = nil
					s.failStage = "/variants"
					args = append(args, "--visibility", "all")
				case "value":
					s.failStage = "/value"
				case "edit scope":
					args[1] = "edit"
					args = append(args, "--label", "new")
					s.failStage = "/" + s.values[0].Variants[0].ID()
				case "edit description":
					args[1] = "edit"
					args = append(args, "--description", "new")
					s.failStage = "/" + testSecretID
				}
				code, out, errout := configurationTest(t, s, args, "replacement")
				if code != 1 || !strings.Contains(out, `"status": "unconfirmed"`) || !strings.Contains(out, "do not retry automatically") {
					t.Fatal(code, out, errout)
				}
				if stage != "name" && !strings.Contains(out, testSecretID) {
					t.Fatal("lost resource receipt", out)
				}
			})
		}
	}
}
func TestConfigurationEmptyStdinSupported(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, input := range []string{"", "\r\n"} {
			s := &configurationServer{values: sampleConfiguration(client.Availability{})}
			code, out, errout := configurationTest(t, s, []string{kind, "set", "TOKEN", "--org", testSecretOrg}, input)
			if code != 0 || len(s.calls) != 2 || s.calls[1].body["value"] != "" {
				t.Fatal(code, out, errout, s.calls)
			}
		}
	}
}

func TestConfigurationDeleteUnconfirmed(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, jsonOutput := range []bool{false, true} {
			for _, wholeName := range []bool{false, true} {
				s := &configurationServer{values: sampleConfiguration(client.Availability{}), code: 503}
				s.failStage = "/" + s.values[0].Variants[0].ID()
				args := []string{kind, "delete", "TOKEN", "--org", testSecretOrg, "--yes"}
				if wholeName {
					args = append(args, "--all")
					s.failStage = "/" + testSecretID
				}
				if jsonOutput {
					args = append(args, "--json", "*")
				}
				code, out, errout := configurationTest(t, s, args, "")
				combined := out + errout
				if code != 1 || !strings.Contains(combined, `"status": "unconfirmed"`) || !strings.Contains(combined, testSecretID) || !strings.Contains(errout, "do not retry automatically") || strings.Contains(combined, "Try again shortly") || strings.Contains(combined, "Output failed") || len(s.calls) != 2 {
					t.Fatal(code, out, errout, s.calls)
				}
			}
		}
	}
}

func TestConfigurationReadCheckoutFallback(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, command := range []string{"list", "get", "set", "edit", "delete"} {
			for _, source := range []string{"checkout", "flag", "config"} {
				t.Run(kind+"/"+command+"/"+source, func(t *testing.T) {
					d, out, stderr := testDeps(t)
					d.GitRemote = func(context.Context) (string, error) { return "git@github.com:acme/unconnected.git", nil }
					d.LoadConfig = func() (config.Config, error) {
						cfg := config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}
						if source == "config" {
							cfg.Repo = "acme/unconnected"
						}
						return cfg, nil
					}
					s := &configurationServer{values: sampleConfiguration(client.Availability{})}
					resolved := 0
					d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
						if strings.HasSuffix(r.URL.Path, "/repos/resolve") {
							resolved++
							return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"detail":"Repository is not connected"}`)), Request: r}, nil
						}
						return s.roundTrip(t, r)
					})}
					args := []string{kind, command}
					if command != "list" {
						args = append(args, "TOKEN")
					}
					if command == "edit" {
						args = append(args, "--label", "new")
					}
					if command == "delete" {
						args = append(args, "--yes")
					}
					if source == "flag" {
						args = append(args, "--repo", "acme/unconnected")
					}
					code := Execute(t.Context(), args, d)
					fallback := source == "checkout" && (command == "list" || command == "get")
					if (code == 0) != fallback || resolved != 1 || strings.Contains(stderr.String(), "showing org-wide results") != fallback {
						t.Fatal(code, out, stderr, resolved)
					}
					if fallback {
						if len(s.calls) != 1 || s.calls[0].method != "GET" || out.Len() == 0 {
							t.Fatal(s.calls, out)
						}
					} else if len(s.calls) != 0 {
						t.Fatal("unexpected configuration request", s.calls)
					}
				})
			}
		}
	}
}

func TestConfigurationVariableOutputModes(t *testing.T) {
	value := "first\n\tsecond\r\x1b]52;c;dGVzdA==\a\x1b[2J\u202eend"
	wantTerminal := "first\n\tsecond" + `\u000d\u001b]52;c;dGVzdA==\u0007\u001b[2J\u202eend`
	for _, mode := range []string{"terminal", "pipe", "file", "json"} {
		t.Run(mode, func(t *testing.T) {
			s := &configurationServer{values: sampleConfiguration(client.Availability{})}
			s.values[0].Variants[0].Value = &value
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { return s.roundTrip(t, r) })}
			d.IsTerminal = func() bool { return mode == "terminal" || mode == "json" }
			args := []string{"variable", "get", "TOKEN", "--org", testSecretOrg}
			var file *os.File
			if mode == "file" {
				var err error
				file, err = os.CreateTemp(t.TempDir(), "value")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = file.Close() }()
				d.Out = file
			}
			if mode == "json" {
				args = append(args, "--json", "value")
			}
			code := Execute(t.Context(), args, d)
			if code != 0 {
				t.Fatal(code, stderr)
			}
			got := out.String()
			if file != nil {
				data, err := os.ReadFile(file.Name())
				if err != nil {
					t.Fatal(err)
				}
				got = string(data)
			}
			if mode == "json" {
				var record struct {
					Value string `json:"value"`
				}
				if err := json.Unmarshal([]byte(got), &record); err != nil || record.Value != value || strings.ContainsRune(got, '\x1b') {
					t.Fatal(got, err)
				}
			} else {
				want := value
				if mode == "terminal" {
					want = wantTerminal
				}
				if got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestConfigurationWriteFailurePrintedOnce(t *testing.T) {
	for _, tc := range []struct {
		name, stage, needle string
		status, exit        int
		pending             bool
	}{
		{"auth", "/value", "HTTP 401", 401, 4, false},
		{"validation", "/variants", "Scope limit reached", 422, 2, false},
		{"uncertain", "/value", "do not retry automatically", 503, 1, false},
		{"pending", "", "write accepted but not yet confirmed", 0, 1, true},
	} {
		for _, mode := range []string{"human", "json", "dotenv"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				s := &configurationServer{values: sampleConfiguration(client.Availability{}), failStage: tc.stage, code: tc.status, pending: tc.pending}
				if tc.stage == "/variants" {
					s.values[0].Variants = nil
				}
				args := []string{"secret", "set", "TOKEN", "--org", testSecretOrg, "--visibility", "all", "--wait", "0s"}
				if mode == "json" {
					args = append(args, "--json", "*")
				}
				if mode == "dotenv" {
					path := filepath.Join(t.TempDir(), "values.env")
					if err := os.WriteFile(path, []byte("TOKEN=synthetic\nOTHER=synthetic\n"), 0600); err != nil {
						t.Fatal(err)
					}
					args = append([]string{"secret", "set", "--env-file", path}, args[3:]...)
				}
				code, out, errout := configurationTest(t, s, args, "synthetic")
				if code != tc.exit || strings.Count(errout, tc.needle) != 1 {
					t.Fatal(code, out, errout)
				}
				if mode == "json" {
					var record configurationResult
					if err := json.Unmarshal([]byte(out), &record); err != nil || !strings.Contains(record.Message, tc.needle) || record.ResourceID != testSecretID {
						t.Fatal(out, err)
					}
				} else {
					if !strings.Contains(errout, testSecretID) || !strings.Contains(errout, "Stage:") {
						t.Fatal(errout)
					}
					if tc.pending && (!strings.Contains(errout, "Check: avr secret get") || !strings.Contains(errout, testVersionID)) {
						t.Fatal(errout)
					}
					if mode == "dotenv" && !strings.Contains(out, "OTHER: unattempted") {
						t.Fatal(out)
					}
				}
			})
		}
	}
}
