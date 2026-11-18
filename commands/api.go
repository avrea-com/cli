package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
	"golang.org/x/net/http/httpguts"
)

func NewAPI(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var method, input, jq string
	var raw, typed, headers []string
	var include, silent bool
	c := &cobra.Command{Use: "api <endpoint>", Short: "Call an Avrea API endpoint.", Args: cobra.ExactArgs(1), Example: "  avr api users/me/organizations\n  avr api /orgs/acme/workflow-runs -X GET -F limit=5\n  avr api /logs/search --input search.json\n  avr api users/me/organizations -i"}
	c.Long = c.Short + "\n\nUse GET by default, or POST when supplying fields or input. With GET, HEAD, or --input, fields become query parameters.\n\n\b\nExamples:\n" + c.Example
	f := c.Flags()
	f.StringVarP(&method, "method", "X", "", "HTTP request method.")
	f.StringArrayVarP(&raw, "raw-field", "f", nil, "Add a literal string field (key=value).")
	f.StringArrayVarP(&typed, "field", "F", nil, "Add a typed field (key=value, @file or @-).")
	f.StringArrayVarP(&headers, "header", "H", nil, "Set a request header (name: value).")
	f.StringVar(&input, "input", "", "Read the body from a file or - for stdin.")
	f.StringVarP(&jq, "jq", "q", "", "Filter the JSON response.")
	f.BoolVarP(&include, "include", "i", false, "Print response status and headers.")
	f.BoolVar(&silent, "silent", false, "Suppress the response body.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		bad := func(s string) error { return &ExitError{2, "Error: " + s} }
		path := args[0]
		// A full URL names another host; a colon in the first segment
		// (users:me) is still a relative endpoint once rooted at "/".
		if u, err := url.Parse(path); path == "" || strings.HasPrefix(path, "//") || strings.Contains(path, "#") || (err == nil && u.Host != "") {
			return bad("expected an endpoint path")
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if _, err := url.Parse(path); err != nil {
			return bad("expected an endpoint path")
		}
		if f.Changed("jq") {
			if silent || jq == "" {
				return bad("--jq requires an expression and response output")
			}
			if _, err := output.CompileJQ(jq); err != nil {
				return bad(err.Error())
			}
		}
		if !f.Changed("method") {
			method = "GET"
			if len(raw)+len(typed) > 0 || f.Changed("input") {
				method = "POST"
			}
		}
		method = strings.ToUpper(method)
		if !httpguts.ValidHeaderFieldName(method) {
			return bad("invalid HTTP method")
		}
		h := make(http.Header)
		for _, line := range headers {
			k, v, ok := strings.Cut(line, ":")
			if !ok || !httpguts.ValidHeaderFieldName(k) || !httpguts.ValidHeaderFieldValue(v) || strings.EqualFold(k, "Host") || strings.EqualFold(k, "Accept-Encoding") {
				return bad("invalid request header")
			}
			h.Add(k, strings.TrimSpace(v))
		}
		uses := 0
		if input == "-" {
			uses++
		}
		for _, field := range typed {
			_, v, _ := strings.Cut(field, "=")
			if v == "@-" {
				uses++
			}
		}
		if uses > 1 {
			return bad("stdin can only be read once")
		}
		read := func(name string) ([]byte, error) {
			if name == "-" {
				return io.ReadAll(d.In)
			}
			return os.ReadFile(name)
		}
		obj := map[string]any{}
		query := url.Values{}
		for idx, fields := range [][]string{raw, typed} {
			for _, field := range fields {
				k, v, ok := strings.Cut(field, "=")
				if !ok || k == "" || strings.ContainsAny(k, "[]") {
					return bad("expected a flat key=value field; use --input for nested values")
				}
				var value any = v
				if idx == 1 {
					switch {
					case strings.HasPrefix(v, "@"):
						b, e := read(v[1:])
						if e != nil {
							return e
						}
						v = string(b)
						value = v
					case v == "true" || v == "false":
						value = v == "true"
					case v == "null":
						value = nil
					case strings.Trim(v, "-0123456789") == "" && json.Valid([]byte(v)):
						value = json.Number(v)
					}
				}
				if method == "GET" || method == "HEAD" || f.Changed("input") {
					query.Add(k, v)
				} else {
					if _, ok := obj[k]; ok {
						return bad("duplicate body field " + k)
					}
					obj[k] = value
				}
			}
		}
		if len(query) > 0 {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			path += sep + query.Encode()
		}
		var body []byte
		var err error
		if f.Changed("input") {
			body, err = read(input)
		} else if len(obj) > 0 {
			body, err = json.Marshal(obj)
		}
		if err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		// An explicit Authorization header is the caller's own credential.
		explicitAuth := len(h.Values("Authorization")) > 0
		if !explicitAuth {
			if err := authenticated(cfg); err != nil {
				return err
			}
		}
		r, err := api.CallAPI(cmd.Context(), method, path, body, h)
		if err != nil {
			return err
		}
		write := func(b []byte) error {
			if d.IsTerminal() {
				b = []byte(output.MultilineText(string(b)))
				if len(b) > 0 && b[len(b)-1] != '\n' {
					b = append(b, '\n')
				}
			}
			_, e := d.Out.Write(b)
			return e
		}
		if include {
			var buf bytes.Buffer
			fmt.Fprintln(&buf, r.Protocol, r.Status)
			if err := r.Headers.Write(&buf); err != nil {
				return err
			}
			buf.WriteString("\r\n")
			if err := write(bytes.ReplaceAll(buf.Bytes(), []byte("\r\n"), []byte("\n"))); err != nil {
				return err
			}
		}
		success := r.Code >= 200 && r.Code < 300
		if !silent {
			if jq != "" && success {
				// 204 No Content and HEAD succeed with nothing to filter.
				if len(r.Body) == 0 {
					return nil
				}
				value, err := client.DecodeJSON(r.Body)
				if err != nil {
					return fmt.Errorf("response is not JSON")
				}
				return output.JSONValue(cmd.Context(), d.Out, value, jq, d.IsTerminal())
			}
			if err := write(r.Body); err != nil {
				return err
			}
		}
		if !success {
			if !explicitAuth && (r.Code == 401 || r.Code == 403) {
				endpoint, _, _ := strings.Cut(path, "?")
				return apiError(cfg, &client.HTTPError{Path: endpoint, Status: r.Code, Reauthentication: r.Headers.Get("X-Reauthentication-Required")}, "call "+method+" "+endpoint)
			}
			return fmt.Errorf("HTTP %d", r.Code)
		}
		return nil
	})
	return c
}
