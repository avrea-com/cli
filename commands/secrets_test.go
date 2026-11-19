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
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

const testSecretOrg = "org-019a0000000070008000000000000001"
const testSecretRepo = "rep-019a0000000070008000000000000001"
const testSecretID = "sec-019a0000000070008000000000000001"
const testVariantID = "vrt-019a0000000070008000000000000001"
const testVersionID = "svv-019a0000000070008000000000000001"

type configurationCall struct {
	method, path string
	body         map[string]any
}
type configurationServer struct {
	document     *contractDocument
	values       []client.ConfigurationValue
	calls        []configurationCall
	failStage    string
	code         int
	pending      bool
	checks       int
	visibility   any
	versionReply *client.ValueWrite
	versionCode  int
}

func (s *configurationServer) roundTrip(t *testing.T, r *http.Request) (*http.Response, error) {
	t.Helper()
	if s.document != nil {
		if _, err := s.document.checkRequest(r); err != nil {
			t.Fatal(err)
		}
	}
	call := configurationCall{method: r.Method, path: r.URL.Path}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&call.body); err != nil {
			t.Fatal(err)
		}
	}
	s.calls = append(s.calls, call)
	value := any(s.values)
	code := 200
	if strings.HasSuffix(call.path, "/value") {
		value = client.ValueWrite{VersionID: testVersionID, Status: "stored"}
		if s.pending {
			code = 202
			value = client.ValueWrite{VersionID: testVersionID, Status: "pending"}
		}
	} else if strings.Contains(call.path, "/versions/") {
		s.checks++
		value = client.ValueWrite{VersionID: testVersionID, Status: "ENABLED"}
		if s.checks == 1 {
			code = 404
			value = map[string]string{"detail": "pending"}
		}
		if s.versionReply != nil {
			value = *s.versionReply
			code = s.versionCode
			if code == 0 {
				code = 200
			}
		}
	} else if strings.Contains(call.path, "/repos/") {
		value = map[string]any{"data": map[string]any{"visibility": s.visibility}}
	} else if r.Method == http.MethodPost && strings.HasSuffix(call.path, "/variants") {
		code = 201
		value = client.ScopedValue{VariantID: testVariantID}
	} else if r.Method == http.MethodPost {
		code = 201
		value = client.ConfigurationValue{SecretID: testSecretID}
	} else if r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
		value = map[string]any{}
	}
	if s.failStage != "" && strings.HasSuffix(call.path, s.failStage) && r.Method != http.MethodGet {
		if s.code == 0 {
			return nil, fmt.Errorf("transport echoed super-secret")
		}
		code = s.code
		value = map[string]string{"detail": "Scope limit reached"}
		if strings.HasSuffix(call.path, "/value") {
			value = map[string]string{"detail": "super-secret"}
		}
	}
	data, _ := json.Marshal(value)
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
}
func configurationTest(t *testing.T, s *configurationServer, args []string, input string) (int, string, string) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}, nil
	}
	d.GitRemote = func(context.Context) (string, error) { return "", fmt.Errorf("no checkout") }
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { return s.roundTrip(t, r) })}
	d.In = strings.NewReader(input)
	d.Wait = func(context.Context, time.Duration) error { return nil }
	code := Execute(t.Context(), args, d)
	return code, out.String(), stderr.String()
}
func sampleConfiguration(scopes ...client.Availability) []client.ConfigurationValue {
	label := "production"
	value := "plain-value"
	ver := 3
	variants := []client.ScopedValue{}
	for i, a := range scopes {
		variants = append(variants, client.ScopedValue{VariantID: fmt.Sprintf("vrt-%032x", i+1), Label: &label, Availability: a, Value: &value, CurrentVersion: &ver, CurrentVersionID: ptr(testVersionID)})
	}
	return []client.ConfigurationValue{{SecretID: testSecretID, Name: "TOKEN", Variants: variants}}
}
func ptr[T any](v T) *T { return &v }

func TestConfigurationSetCreatesNameScopeAndValue(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		t.Run(kind, func(t *testing.T) {
			s := &configurationServer{values: []client.ConfigurationValue{}}
			code, out, errout := configurationTest(t, s, []string{kind, "set", "token", "--repo", testSecretRepo, "--env", "Production", "--branch", "main", "--workflow", ".avrea/workflows/deploy.yml", "--label", "live", "--json", "*"}, "super-secret\n")
			if code != 0 {
				t.Fatal(code, out, errout)
			}
			if len(s.calls) != 4 {
				t.Fatal(s.calls)
			}
			if s.calls[1].body["name"] != "TOKEN" || s.calls[3].body["value"] != "super-secret" {
				t.Fatal(s.calls)
			}
			a := s.calls[2].body["availability"].(map[string]any)
			if a["repositories"].([]any)[0] != testSecretRepo || a["workflows"].([]any)[0] != ".avrea/workflows/deploy.yml" {
				t.Fatal(a)
			}
			if strings.Contains(out+errout, "super-secret") || !strings.Contains(out, "saved") {
				t.Fatal(out, errout)
			}
		})
	}
}
func TestConfigurationCreationRequiresExplicitOrganizationAvailability(t *testing.T) {
	s := &configurationServer{values: []client.ConfigurationValue{}}
	code, _, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg}, "value")
	if code != 2 || !strings.Contains(errout, "explicit") || len(s.calls) != 1 {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationSetPreservesScopeAndSelectsExactly(t *testing.T) {
	a := client.Availability{Repositories: []string{testSecretRepo}, Environments: []string{"production"}, Branches: []string{"main"}}
	s := &configurationServer{values: sampleConfiguration(a)}
	code, _, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo}, "replacement")
	if code != 0 || len(s.calls) != 2 || !strings.HasSuffix(s.calls[1].path, "/value") {
		t.Fatal(code, errout, s.calls)
	}
	s = &configurationServer{values: sampleConfiguration(a)}
	code, _, errout = configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo, "--env", "PRODUCTION", "--branch", "main"}, "replacement")
	if code != 0 || len(s.calls) != 2 {
		t.Fatal(code, errout, s.calls)
	}
	s = &configurationServer{values: sampleConfiguration(a)}
	code, _, errout = configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo, "--variant", "production", "--env", "staging"}, "replacement")
	if code != 2 || len(s.calls) != 1 {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationRepositorySetDoesNotOverwriteInherited(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{})}
	code, _, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo}, "replacement")
	if code != 0 || len(s.calls) != 3 || s.calls[1].method != "POST" || !strings.HasSuffix(s.calls[1].path, "/variants") {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationUnknownVariantNeverCreates(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{})}
	code, _, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--variant", "typo", "--org", testSecretOrg}, "replacement")
	if code != 2 || len(s.calls) != 1 {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationAmbiguousMutationsRequireSelection(t *testing.T) {
	for _, command := range [][]string{{"set", "TOKEN"}, {"edit", "TOKEN", "--label", "new"}, {"delete", "TOKEN", "--yes"}} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{}, client.Availability{Environments: []string{"prod"}})}
		args := append([]string{"secret"}, command...)
		args = append(args, "--org", testSecretOrg)
		code, _, errout := configurationTest(t, s, args, "replacement")
		if code != 2 || len(s.calls) != 1 {
			t.Fatal(code, errout, s.calls)
		}
	}
}
func TestConfigurationSecretReadsAreMetadataOnly(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{}, client.Availability{Environments: []string{"prod"}})}
	code, out, errout := configurationTest(t, s, []string{"secret", "get", "TOKEN", "--org", testSecretOrg, "--json", "*"}, "")
	if code != 0 || strings.Contains(out, "plain-value") || !strings.Contains(out, testVersionID) || !strings.Contains(out, "current_version") {
		t.Fatal(code, out, errout)
	}
}
func TestConfigurationVariableGetIsExactPlaintext(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{})}
	code, out, errout := configurationTest(t, s, []string{"variable", "get", "TOKEN", "--org", testSecretOrg}, "")
	if code != 0 || out != "plain-value" {
		t.Fatal(code, out, errout)
	}
}
func TestConfigurationPendingWritePollsWithoutAnotherPut(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{}), pending: true}
	code, out, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg, "--verbose"}, "super-secret")
	if code != 0 || s.checks != 2 || len(s.calls) != 4 || strings.Contains(out+errout, "super-secret") {
		t.Fatal(code, out, errout, s.calls)
	}
}
func TestConfigurationPendingWriteRetainsReceipt(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{}), pending: true}
	code, out, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg, "--wait", "0s", "--json", "*"}, "super-secret")
	if code != 1 || !strings.Contains(out, testVersionID) || !strings.Contains(out, "unconfirmed") || len(s.calls) != 2 || strings.Contains(out+errout, "super-secret") {
		t.Fatal(code, out, errout, s.calls)
	}
}
func TestConfigurationPartialCreationAndLostResponses(t *testing.T) {
	for _, stage := range []string{"/variants", "/value"} {
		for _, status := range []int{0, 401, 403, 409, 422, 503} {
			t.Run(fmt.Sprint(stage, status), func(t *testing.T) {
				s := &configurationServer{values: []client.ConfigurationValue{}, failStage: stage, code: status}
				code, out, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo, "--json", "*", "--verbose"}, "super-secret")
				if code == 0 || !strings.Contains(out, testSecretID) || strings.Contains(out+errout, "super-secret") {
					t.Fatal(code, out, errout)
				}
				expected := 3
				if stage == "/value" {
					expected = 4
				}
				if len(s.calls) != expected {
					t.Fatal(s.calls)
				}
				if status == 0 && !strings.Contains(out, "unconfirmed") {
					t.Fatal(out)
				}
			})
		}
	}
}
func TestConfigurationEditPreservesUnspecifiedFields(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{Environments: []string{"prod"}, Branches: []string{"main"}})}
	s.values[0].Variants[0].RotationReminderDays = ptr(30)
	code, _, errout := configurationTest(t, s, []string{"secret", "edit", "TOKEN", "--org", testSecretOrg, "--clear-branch", "--clear-label"}, "")
	if code != 0 || len(s.calls) != 2 {
		t.Fatal(code, errout, s.calls)
	}
	body := s.calls[1].body
	a := body["availability"].(map[string]any)
	if body["label"] != nil || body["rotation_reminder_days"] != float64(30) || a["branches"] != nil || a["environments"].([]any)[0] != "prod" {
		t.Fatal(body)
	}
}
func TestConfigurationDescriptionEditDoesNotSelectVariant(t *testing.T) {
	s := &configurationServer{values: sampleConfiguration(client.Availability{}, client.Availability{Environments: []string{"prod"}})}
	code, _, errout := configurationTest(t, s, []string{"secret", "edit", "TOKEN", "--org", testSecretOrg, "--description", ""}, "")
	if code != 0 || len(s.calls) != 2 || s.calls[1].method != "PATCH" || s.calls[1].body["description"] != nil {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationDeleteWholeNameIsExplicit(t *testing.T) {
	for _, all := range []bool{false, true} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{})}
		args := []string{"secret", "delete", "TOKEN", "--org", testSecretOrg, "--yes"}
		if all {
			args = append(args, "--all")
		}
		code, _, errout := configurationTest(t, s, args, "")
		if code != 0 || len(s.calls) != 2 || s.calls[1].method != "DELETE" {
			t.Fatal(code, errout, s.calls)
		}
		if strings.Contains(s.calls[1].path, "/secret-variants/") == all {
			t.Fatal(s.calls)
		}
	}
}
func TestConfigurationPrivateApplicabilityUsesKnownVisibility(t *testing.T) {
	for _, visibility := range []any{"private", "public", "internal", nil} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{PrivateOnly: true}), visibility: visibility}
		code, out, errout := configurationTest(t, s, []string{"secret", "list", "--repo", testSecretRepo, "--json", "*"}, "")
		if visibility == nil {
			if code != 0 || strings.Contains(out, "TOKEN") || !strings.Contains(errout, "private-only values were omitted") {
				t.Fatal(code, out, errout)
			}
			continue
		}
		if code != 0 || strings.Contains(out, "TOKEN") != (visibility == "private") {
			t.Fatal(code, out, errout)
		}
	}
}
func TestConfigurationBulkPreflightAndPartialResults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "values.env")
	if err := os.WriteFile(file, []byte("FIRST=super-secret\nSECOND=another\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &configurationServer{values: []client.ConfigurationValue{}, failStage: "/value", code: 0}
	code, out, errout := configurationTest(t, s, []string{"secret", "set", "--env-file", file, "--repo", testSecretRepo, "--json", "*"}, "")
	if code != 1 || !strings.Contains(out, "unattempted") || !strings.Contains(out, "SECOND") || strings.Contains(out+errout, "super-secret") || len(s.calls) != 4 {
		t.Fatal(code, out, errout, s.calls)
	}
	if err := os.WriteFile(file, []byte("FIRST=okay\nFIRST=duplicate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s = &configurationServer{}
	code, _, errout = configurationTest(t, s, []string{"secret", "set", "--env-file", file, "--repo", testSecretRepo}, "")
	if code != 2 || len(s.calls) != 0 {
		t.Fatal(code, errout, s.calls)
	}
}
func TestConfigurationValueBoundaries(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", configurationValueLimit+1), "bad\x00value", string([]byte{255})} {
		s := &configurationServer{values: []client.ConfigurationValue{}}
		code, _, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--repo", testSecretRepo}, value)
		if code != 2 || len(s.calls) != 1 {
			t.Fatal(code, errout, s.calls)
		}
	}
}
func TestConfigurationDotenvLiteralQuotesAndMultiline(t *testing.T) {
	file := filepath.Join(t.TempDir(), "literal.env")
	if err := os.WriteFile(file, []byte("export TOKEN='line one\nline two'\nCONFIG=\"a\\nb\" # comment\nLITERAL=$TOKEN\nEMPTY=\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := configurationEnvFile(file)
	if err != nil {
		t.Fatal(err)
	}
	expected := []configurationInput{{"TOKEN", "line one\nline two"}, {"CONFIG", "a\nb"}, {"LITERAL", "$TOKEN"}, {"EMPTY", ""}}
	if fmt.Sprint(got) != fmt.Sprint(expected) {
		t.Fatal(got)
	}
}

func TestAPIContractConfiguration(t *testing.T) {
	document := loadContractDocument(t)
	for _, kind := range []string{"secret", "variable"} {
		for _, args := range [][]string{
			{"set", "TOKEN", "--visibility", "all"},
			{"get", "TOKEN"},
			{"list"},
			{"edit", "TOKEN", "--clear-label"},
			{"edit", "TOKEN", "--description", "Updated"},
			{"delete", "TOKEN", "--yes"},
			{"delete", "TOKEN", "--all", "--yes"},
		} {
			t.Run(kind+strings.Join(args, " "), func(t *testing.T) {
				values := sampleConfiguration(client.Availability{})
				if args[0] == "set" {
					values = []client.ConfigurationValue{}
				}
				s := &configurationServer{values: values, document: &document}
				command := append([]string{kind}, args...)
				command = append(command, "--org", testSecretOrg)
				code, out, errout := configurationTest(t, s, command, "synthetic")
				if code != 0 {
					t.Fatal(code, out, errout)
				}
			})
		}
	}
	s := &configurationServer{values: sampleConfiguration(client.Availability{}), document: &document, checks: 1}
	code, out, errout := configurationTest(t, s, []string{"secret", "get", "TOKEN", "--org", testSecretOrg, "--version-id", testVersionID}, "")
	if code != 0 {
		t.Fatal(code, out, errout)
	}
}

func TestConfigurationStdinLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name, kind   string
		args         []string
		existingFile bool
		want         string
	}{
		{"secret", "secret", nil, false, "  token\nline "},
		{"variable", "variable", nil, false, "  token\nline "},
		{"new-file", "secret", []string{"--type", "file"}, false, "  token\nline \r\n\n"},
		{"existing-file", "secret", nil, true, "  token\nline \r\n\n"},
		{"body", "variable", []string{"--body", "  token\nline \r\n\n"}, false, "  token\nline \r\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &configurationServer{values: []client.ConfigurationValue{}}
			if tc.existingFile {
				s.values = sampleConfiguration(client.Availability{Repositories: []string{testSecretRepo}})
				s.values[0].AsFile = true
			}
			args := append([]string{tc.kind, "set", "TOKEN", "--repo", testSecretRepo}, tc.args...)
			code, out, errout := configurationTest(t, s, args, "  token\nline \r\n\n")
			if code != 0 || s.calls[len(s.calls)-1].body["value"] != tc.want {
				t.Fatal(code, out, errout, s.calls)
			}
		})
	}
}
func TestConfigurationCreateDescription(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, args := range [][]string{nil, {"--description", ""}, {"--description", "Usage notes"}} {
			s := &configurationServer{values: []client.ConfigurationValue{}}
			command := append([]string{kind, "set", "TOKEN", "--repo", testSecretRepo}, args...)
			code, out, errout := configurationTest(t, s, command, "value")
			if code != 0 {
				t.Fatal(code, out, errout)
			}
			var want any
			if len(args) > 0 && args[1] != "" {
				want = args[1]
			}
			if s.calls[1].body["description"] != want {
				t.Fatal(s.calls[1])
			}
		}
	}
}
func TestConfigurationWholeNameDeleteRejectsAmbientOrg(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{})}
		code, _, errout := configurationTest(t, s, []string{kind, "delete", "TOKEN", "--all", "--yes"}, "")
		if code != 2 || !strings.Contains(errout, "explicit --org") {
			t.Fatal(code, errout)
		}
		for _, call := range s.calls {
			if call.method == "DELETE" {
				t.Fatal(call)
			}
		}
	}
}
func TestConfigurationUnknownPrivacyKeepsKnownScopes(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		for _, verb := range []string{"list", "get"} {
			s := &configurationServer{values: sampleConfiguration(client.Availability{PrivateOnly: true})}
			known := sampleConfiguration(client.Availability{Repositories: []string{testSecretRepo}})[0]
			known.Name = "KNOWN"
			s.values = append(s.values, known)
			args := []string{kind, verb}
			if verb == "get" {
				args = append(args, "KNOWN")
			}
			args = append(args, "--repo", testSecretRepo, "--json", "*")
			code, out, errout := configurationTest(t, s, args, "")
			if code != 0 || strings.Contains(out, "TOKEN") || (strings.Contains(errout, "private-only values were omitted") != (verb == "list")) {
				t.Fatal(code, out, errout)
			}
			if verb == "list" && !strings.Contains(out, "KNOWN") {
				t.Fatal(out)
			}
		}
	}
}
func TestConfigurationPluralAliases(t *testing.T) {
	for _, kind := range []string{"secrets", "variables"} {
		s := &configurationServer{values: sampleConfiguration(client.Availability{})}
		code, out, errout := configurationTest(t, s, []string{kind, "list", "--org", testSecretOrg}, "")
		if code != 0 || !strings.Contains(out, "TOKEN") {
			t.Fatal(code, out, errout)
		}
	}
}
func TestConfigurationRepositoryIDShape(t *testing.T) {
	for _, flag := range []string{"--repo", "--repos"} {
		for _, id := range []string{"rep-typo", "rep-" + strings.Repeat("a", 31), "rep-" + strings.Repeat("A", 32), testSecretRepo} {
			s := &configurationServer{values: []client.ConfigurationValue{}}
			code, out, errout := configurationTest(t, s, []string{"secret", "set", "TOKEN", "--org", testSecretOrg, flag, id}, "value")
			if id == testSecretRepo {
				if code != 0 {
					t.Fatal(code, out, errout)
				}
			} else if code != 2 {
				t.Fatal(id, code, out, errout)
			}
			for _, call := range s.calls {
				if strings.Contains(call.path, "/repos/") {
					t.Fatal("ID validation performed a lookup", call)
				}
				if id != testSecretRepo && call.method != "GET" {
					t.Fatal("malformed scope was saved", call)
				}
			}
		}
	}
}

func TestConfigurationMetadataValidationExplainsFailure(t *testing.T) {
	for _, kind := range []string{"secret", "variable"} {
		s := &configurationServer{values: []client.ConfigurationValue{}, failStage: "/variants", code: 422}
		code, out, errout := configurationTest(t, s, []string{kind, "set", "TOKEN", "--repo", testSecretRepo}, "secret-value")
		if code == 0 || !strings.Contains(errout, "Scope limit reached") || strings.Contains(out+errout, "secret-value") {
			t.Fatal(code, out, errout)
		}
	}
}
func TestConfigurationRepositoryLookupChecksLaterPages(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		cfg := config.Config{Host: "https://api.example.test", Token: "fixture", Org: testSecretOrg}
		d.LoadConfig = func() (config.Config, error) { return cfg, nil }
		pages, writes := 0, 0
		d.In = strings.NewReader("token")
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/repos") {
				pages++
				full := "acme/web"
				var cursor any = "next"
				if pages == 2 {
					if r.URL.Query().Get("cursor") != "next" {
						t.Fatal(r.URL)
					}
					cursor = nil
					full = "acme/worker"
					if ambiguous {
						full = "acme/subgroup/web"
					}
				}
				body, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"repository_id": fmt.Sprintf("rep-%032x", pages), "full_name": full}}, "pagination": map[string]any{"next_cursor": cursor}})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			}
			if r.Method != http.MethodGet {
				writes++
			}
			s := &configurationServer{values: []client.ConfigurationValue{}}
			return s.roundTrip(t, r)
		})}
		code := Execute(t.Context(), []string{"secret", "set", "TOKEN", "--repos", "web", "--org", testSecretOrg}, d)
		if pages != 2 {
			t.Fatal(pages, code, out, stderr)
		}
		if ambiguous {
			if code != 2 || writes != 0 || !strings.Contains(stderr.String(), "ambiguous") {
				t.Fatal(code, writes, out, stderr)
			}
		} else if code != 0 || writes != 3 {
			t.Fatal(code, writes, out, stderr)
		}
	}
}
