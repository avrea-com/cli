package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestLocalIntrospectionDoesNotLoadContext(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"repo", "list", "--help"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) { t.Fatal("loaded config for introspection"); return config.Config{}, nil }
			d.GitRemote = func(context.Context) (string, error) { t.Fatal("started git for introspection"); return "", nil }
			if code := Execute(t.Context(), args, d); code != 0 {
				t.Fatal(code, stderr)
			}
		})
	}
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) { t.Fatal("loaded config for completion"); return config.Config{}, nil }
	d.GitRemote = func(context.Context) (string, error) { t.Fatal("started git for completion"); return "", nil }
	d.Getenv = func(key string) string {
		return map[string]string{"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr repo ", "COMP_CWORD": "2"}[key]
	}
	if code := Execute(t.Context(), nil, d); code != 0 {
		t.Fatal(code, stderr)
	}
}

// Keep providers lazy without a process-global cache: subsequent invocations
// must see changed credentials/configuration, including embedded admin usage.
func TestInvocationLoadsConfigOnceAndRefreshes(t *testing.T) {
	d, out, stderr := testDeps(t)
	loads, requests := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != fmt.Sprintf("Bearer avr_%d", requests) {
			t.Errorf("stale token: %s", got)
		}
		_, _ = fmt.Fprintf(w, `{"email":"user%d@example.test"}`, requests)
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		loads++
		return config.Config{Host: server.URL, Token: fmt.Sprintf("avr_%d", loads)}, nil
	}
	d.GitRemote = func(context.Context) (string, error) { t.Fatal("unnecessary git detection"); return "", nil }
	for i := 1; i <= 2; i++ {
		out.Reset()
		if code := Execute(t.Context(), []string{"auth", "status", "--json", "email", "--jq", ".email"}, d); code != 0 {
			t.Fatal(code, stderr)
		}
		if loads != i || requests != i || strings.TrimSpace(out.String()) != fmt.Sprintf("user%d@example.test", i) {
			t.Fatal(loads, requests, out)
		}
	}
}

func TestBootstrapTemplatesAllParseOnDemand(t *testing.T) {
	if _, err := bootstrapTemplates(); err != nil {
		t.Fatal(err)
	}
}
