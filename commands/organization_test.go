package commands

import (
	"context"
	"net/http"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestOrganizationMutationsCancelledBeforeRequest(t *testing.T) {
	for _, args := range [][]string{
		{"org", "create", "Fixture Team"},
		{"org", "email-domain", "claim", "example.test"},
		{"org", "email-domain", "verify", "example.test"},
		{"org", "email-domain", "set", "example.test", "--yes"},
	} {
		t.Run(args[1]+" "+args[2], func(t *testing.T) {
			d, _, stderr := testDeps(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d.LoadConfig = func() (config.Config, error) {
				cancel()
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("cancelled command reached the API")
				return nil, nil
			})}
			if code := Execute(ctx, args, d); code != 130 {
				t.Fatal(code, stderr)
			}
		})
	}
}
