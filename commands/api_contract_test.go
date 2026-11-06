package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

// This intentionally checks only request routes, methods, query names, required
// queries, primitive types/bounds and enums. It is not a JSON Schema validator.
// Parameter serialization: https://spec.openapis.org/oas/v3.1.0#parameter-object
// Keep this test-only; no schema or generated records ship in the executable.
type contractSchema struct {
	Ref     string            `json:"$ref"`
	Type    string            `json:"type"`
	Enum    []string          `json:"enum"`
	AnyOf   []*contractSchema `json:"anyOf"`
	Items   *contractSchema   `json:"items"`
	Minimum *float64          `json:"minimum"`
	Maximum *float64          `json:"maximum"`
}
type contractParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   contractSchema `json:"schema"`
}
type contractOperation struct {
	Parameters []contractParameter `json:"parameters"`
}
type contractDocument struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]*contractSchema `json:"schemas"`
	} `json:"components"`
}

func (d contractDocument) checkValue(s *contractSchema, values []string) error {
	if s == nil {
		return fmt.Errorf("missing parameter schema")
	}
	if s.Ref != "" {
		prefix := "#/components/schemas/"
		target := d.Components.Schemas[strings.TrimPrefix(s.Ref, prefix)]
		if !strings.HasPrefix(s.Ref, prefix) || target == nil {
			return fmt.Errorf("unsupported schema reference %s", s.Ref)
		}
		return d.checkValue(target, values)
	}
	if len(s.AnyOf) != 0 {
		for _, branch := range s.AnyOf {
			if d.checkValue(branch, values) == nil {
				return nil
			}
		}
		return fmt.Errorf("value does not match any parameter alternative")
	}
	if s.Type == "array" {
		for _, value := range values {
			if err := d.checkValue(s.Items, []string{value}); err != nil {
				return err
			}
		}
		return nil
	}
	if len(values) != 1 {
		return fmt.Errorf("repeated scalar parameter")
	}
	value := values[0]
	if len(s.Enum) != 0 && !slices.Contains(s.Enum, value) {
		return fmt.Errorf("%q is not in %v", value, s.Enum)
	}
	switch s.Type {
	case "null":
		return fmt.Errorf("expected null")
	case "integer", "number":
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		if s.Type == "integer" {
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				return err
			}
		}
		if s.Minimum != nil && number < *s.Minimum || s.Maximum != nil && number > *s.Maximum {
			return fmt.Errorf("number outside parameter bounds")
		}
	case "boolean":
		if value != "true" && value != "false" {
			return fmt.Errorf("expected boolean")
		}
	}
	return nil
}

func (d contractDocument) checkRequest(r *http.Request) (string, error) {
	path, specificity := "", -1
	parts := strings.Split(r.URL.EscapedPath(), "/")
	for template := range d.Paths {
		segments := strings.Split(template, "/")
		if len(segments) != len(parts) {
			continue
		}
		score, matches := 0, true
		for i, segment := range segments {
			if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
				if parts[i] == "" {
					matches = false
				}
			} else if segment == parts[i] {
				score++
			} else {
				matches = false
			}
		}
		if matches && score > specificity {
			path, specificity = template, score
		}
	}
	if path == "" {
		return "", fmt.Errorf("route missing from OpenAPI: %s", r.URL.Path)
	}
	operation := r.Method + " " + path
	item := d.Paths[path]
	raw, ok := item[strings.ToLower(r.Method)]
	if !ok {
		return operation, fmt.Errorf("method missing from OpenAPI: %s", operation)
	}
	var op contractOperation
	if err := json.Unmarshal(raw, &op); err != nil {
		return operation, err
	}
	var shared []contractParameter
	if raw := item["parameters"]; raw != nil {
		if err := json.Unmarshal(raw, &shared); err != nil {
			return operation, err
		}
	}
	params := map[string]contractParameter{}
	for _, p := range append(shared, op.Parameters...) {
		if p.In == "query" {
			params[p.Name] = p
		}
	}
	query := r.URL.Query()
	for name, p := range params {
		if p.Required && !query.Has(name) {
			return operation, fmt.Errorf("required query missing: %s", name)
		}
	}
	for name, values := range query {
		p, ok := params[name]
		if !ok {
			return operation, fmt.Errorf("query absent from OpenAPI: %s", name)
		}
		if err := d.checkValue(&p.Schema, values); err != nil {
			return operation, fmt.Errorf("query %s: %w", name, err)
		}
	}
	return operation, nil
}

func TestAPIContract(t *testing.T) {
	path := os.Getenv("AVR_TEST_OPENAPI")
	if path == "" {
		t.Skip("make go-api-check AVREA_CORE=/path/to/avrea-core fetches the current schema")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document contractDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Paths) == 0 {
		t.Fatal("OpenAPI has no paths")
	}
	const org = "/orgs/{org_id}"
	const repo = "rep-019a0000000070008000000000000002"
	// Each command must reach its intended operation. The transport supplies the
	// PR feature flag and rejects target requests, including mutations. No API,
	// browser, SSH or subprocess is needed.
	// Add cases when adding request shapes; this is representative, not exhaustive.
	cases := []struct{ args, operation string }{
		{"auth status", "GET /users/me"},
		{"org list", "GET /users/me/organizations"},
		{"repo list", "GET " + org + "/repos"},
		{"pr list", "GET " + org + "/pull-requests"},
		{"pr list --scope authored --state closed --limit 17 --cursor next", "GET " + org + "/pull-requests"},
		{"pr list --scope involved", "GET " + org + "/pull-requests"},
		{"pr list --scope rest", "GET " + org + "/pull-requests"},
		{"pr list --scope attention", "GET " + org + "/pull-requests"},
		{"run list --status success --limit 17 --cursor next", "GET " + org + "/workflow-runs"},
		{"run view " + inspectionRun, "GET " + org + "/workflow-runs/{run_id}"},
		{"job list --limit 17 --cursor next", "GET " + org + "/jobs"},
		{"job view " + inspectionJob, "GET " + org + "/jobs/{job_id}"},
		{"cache usage --repo " + repo, "GET " + org + "/repos/{repo_id}/cache"},
		{"cache delete --repo " + repo + " --all --yes", "DELETE " + org + "/repos/{repo_id}/cache"},
		{"vm list", "GET " + org + "/vms"},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://contract.invalid", Token: "fixture", Org: inspectionOrg, Repo: repo}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				operation, err := document.checkRequest(r)
				if err != nil {
					t.Errorf("%s: %v", r.URL, err)
				}
				if r.Method == "GET" && r.URL.Path == "/orgs/"+inspectionOrg+"/feature-flags/"+prFeature {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"enabled":true}`)), Request: r}, nil
				}
				calls++
				if operation != tc.operation {
					t.Errorf("reached %s, want %s", operation, tc.operation)
				}
				return &http.Response{StatusCode: http.StatusTeapot, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"detail":"contract probe stopped"}`)), Request: r}, nil
			})}
			if code := Execute(t.Context(), strings.Fields(tc.args), d); code == 0 || calls != 1 {
				t.Fatalf("code=%d, requests=%d: %s", code, calls, stderr)
			}
		})
	}
}

func TestAPIContractDetectsDrift(t *testing.T) {
	var d contractDocument
	err := json.Unmarshal([]byte(`{"paths":{"/items/{id}":{"parameters":[{"in":"query","name":"limit","schema":{"type":"integer","minimum":1,"maximum":20}}],"get":{"parameters":[{"in":"query","name":"scope","required":true,"schema":{"anyOf":[{"type":"array","items":{"$ref":"#/components/schemas/Scope"}},{"type":"null"}]}}]}}},"components":{"schemas":{"Scope":{"type":"string","enum":["authored","involved"]}}}}`), &d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		valid        bool
	}{
		{"GET", "/items/id?scope=authored&scope=involved&limit=17", true},
		{"GET", "/removed/id?scope=authored", false},
		{"DELETE", "/items/id?scope=authored", false},
		{"GET", "/items/id", false},
		{"GET", "/items/id?scope=all", false},
		{"GET", "/items/id?scope=authored&unknown=x", false},
		{"GET", "/items/id?scope=authored&limit=21", false},
		{"GET", "/items/id?scope=authored&limit=bad", false},
	} {
		_, err := d.checkRequest(httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		if (err == nil) != tc.valid {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
		}
	}
}
