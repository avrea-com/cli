package commands

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestBrowserArguments(t *testing.T) {
	target := "https://api.test/oauth/github/login?next=http://localhost:8765/callback&literal=$(touch)"
	for _, tt := range []struct {
		command string
		want    []string
	}{
		{`"C:\Program Files\Browser\browser.exe" --new-tab "%s"`, []string{`C:\Program Files\Browser\browser.exe`, "--new-tab", target}},
		{`browser --flag`, []string{"browser", "--flag", target}},
		{`browser 'literal $HOME $(touch file)' %s`, []string{"browser", "literal $HOME $(touch file)", target}},
	} {
		args, err := browserArgs(tt.command, target)
		if err != nil || !reflect.DeepEqual(args, tt.want) {
			t.Fatalf("%q: %#v %v", tt.command, args, err)
		}
	}
	if _, err := browserArgs(`browser "unterminated`, target); err == nil {
		t.Fatal("accepted malformed browser command")
	}
}

func TestJQHasNoExecutableDependency(t *testing.T) {
	t.Setenv("PATH", "")
	d, out, stderr := testDeps(t)
	d.Getenv = func(key string) string {
		if key == "AVR_TOKEN" {
			return "avs_fixture"
		}
		return ""
	}
	if code := Execute(context.Background(), []string{"auth", "status", "--json", "credential_type", "--jq", ".credential_type"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if strings.TrimSpace(out.String()) != "scoped_token" {
		t.Fatal(out)
	}
}
