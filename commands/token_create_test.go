package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const createdTokenFixture = "avs_created_fixture_secret"

func tokenMintFixture() map[string]any {
	return map[string]any{"id": "key-" + "019a0000000070008000000000000004", "organization_id": inspectionOrg, "token": createdTokenFixture, "name": "ci", "grants": []any{}}
}
func TestTokenCreateCancelledFilterRecoversCredentialWithExit130(t *testing.T) {
	d, out, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := deliverCreatedToken(ctx, d, tokenMintFixture(), createdTokenFixture, inspectionOrg, true, []string{"token", "id"}, ".token")
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 130 || !strings.Contains(err.Error(), "unfiltered JSON") || strings.Count(out.String(), createdTokenFixture) != 1 || strings.Contains(err.Error()+stderr.String(), createdTokenFixture) {
		t.Fatal(err, out, stderr)
	}
}

type tokenBrokenWriter struct{}

func (tokenBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type tokenFlushFailure struct{ bytes.Buffer }

func (w *tokenFlushFailure) Flush() error { return io.ErrClosedPipe }
func TestTokenCreateOutputFailureKeepsNonzeroExitAndDoesNotClaimRecovery(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, out := range []io.Writer{tokenBrokenWriter{}, &tokenFlushFailure{}} {
			d, _, stderr := testDeps(t)
			d.Out = out
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				data, err := json.Marshal(tokenMintFixture())
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
			})}
			args := []string{"token", "create", "--name", "ci", "--allow-vm-create"}
			if jsonOutput {
				args = append(args, "--json", "token,id")
			}
			code := Execute(t.Context(), args, d)
			message := stderr.String()
			if code != 1 || calls != 1 || !strings.Contains(message, "avr token revoke") || strings.Contains(message, createdTokenFixture) || strings.Contains(message, "stdout contains") || strings.Contains(message, "export lines") {
				t.Fatal(code, calls, message)
			}
		}
	}
}
func TestTokenCreateUnknownOutcomeIsNotRetriedOrLeaked(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if interrupted {
				cancel()
				return nil, ctx.Err()
			}
			return nil, errors.New("unsafe " + createdTokenFixture)
		})}
		code := Execute(ctx, []string{"token", "create", "--name", "ci", "--allow-vm-create"}, d)
		cancel()
		expected := 1
		if interrupted {
			expected = 130
		}
		if code != expected || calls != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "may have been created") || strings.Contains(stderr.String(), createdTokenFixture) {
			t.Fatal(code, calls, out, stderr)
		}
	}
}
