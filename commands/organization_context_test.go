package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestOrganizationResolutionReusedOnlyWithinInvocation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = fmt.Fprintf(w, `{"data":[{"organization_id":"org-fixture","slug":"slug%d"}]}`, calls)
	}))
	defer server.Close()
	d, _, stderr := testDeps(t)
	cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
	root := NewRoot(d)
	root.AddCommand(&cobra.Command{Use: "probe", RunE: func(c *cobra.Command, _ []string) error {
		api := d.defaults().apiClient(cfg)
		id, err := resolveOrg(c.Context(), d.defaults(), cfg, api, "")
		if err != nil {
			return err
		}
		for range 2 {
			if slug := orgSlug(c.Context(), cfg, api, id); slug != fmt.Sprintf("slug%d", calls) {
				t.Errorf("slug=%s calls=%d", slug, calls)
			}
		}
		return nil
	}})
	for want := 1; want <= 2; want++ {
		if code := ExecuteRoot(t.Context(), root, []string{"probe"}, d.Err, d); code != 0 {
			t.Fatal(code, stderr)
		}
		if calls != want {
			t.Fatalf("wanted %d organization requests, got %d", want, calls)
		}
	}
}

func TestOrganizationSlugScope(t *testing.T) {
	ctx := context.WithValue(t.Context(), organizationSlugsKey{}, &organizationSlugs{})
	cfg := config.Config{Host: "https://api.example.test", Token: "avr_one"}
	rememberOrganizationSlug(ctx, cfg, "org-fixture", "first")
	for _, other := range []config.Config{{Host: cfg.Host, Token: "avr_two"}, {Host: "https://other.example.test", Token: cfg.Token}} {
		if got := knownOrganizationSlug(ctx, other, "org-fixture"); got != "" {
			t.Fatal("crossed credential or host boundary", got)
		}
	}
	if got := knownOrganizationSlug(t.Context(), cfg, "org-fixture"); got != "" {
		t.Fatal("escaped invocation", got)
	}
}
