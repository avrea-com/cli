package commands

import (
	"context"
	"net/http"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestPublicMirrorMutationsCancelledBeforeRequest(t *testing.T) {
	for _, args := range [][]string{
		{"repo", "public-mirror", "request", "rust-lang/rust"},
		{"repo", "public-mirror", "cancel", "pmr-019a0000000070008000000000000004", "--yes"},
	} {
		t.Run(args[2], func(t *testing.T) {
			d, _, stderr := testDeps(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d.LoadConfig = func() (config.Config, error) {
				cancel()
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("cancelled command reached API")
				return nil, nil
			})}
			if code := Execute(ctx, args, d); code != 130 {
				t.Fatal(code, stderr)
			}
		})
	}
}
