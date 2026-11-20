package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

type cardFlowTransport func(*http.Request) (*http.Response, error)

func (f cardFlowTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const (
	cardAddOrg      = "org-0196f0c0a1b27c3d8e9f0a1b2c3d4e5f"
	cardAddPage     = "https://console.example.test/org/acme/settings/billing"
	existingCard    = `{"payment_method_id":"pm_existing","card_brand":"mastercard","card_last4":"4444","card_exp_month":1,"card_exp_year":2027,"is_default":true}`
	addedCard       = `{"payment_method_id":"pm_added","card_brand":"visa","card_last4":"3155","card_exp_month":3,"card_exp_year":2028,"is_default":false}`
	cardListWithout = `{"data":[` + existingCard + `]}`
	cardListWith    = `{"data":[` + existingCard + `,` + addedCard + `]}`
)

// runCardAdd drives `billing payment-methods add` against a fake API. lists
// are the successive payment-method list responses, the first one taken
// before the console opens; the last one repeats.
func runCardAdd(t *testing.T, terminal bool, lists ...string) (int, string, []string, int) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture_private"}, nil
	}
	d.IsTerminal = func() bool { return terminal }
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	d.Now = func() time.Time { return now }
	d.Wait = func(_ context.Context, delay time.Duration) error { now = now.Add(delay); return nil }
	var opened []string
	d.OpenBrowser = func(_ context.Context, target string) error { opened = append(opened, target); return nil }
	reads := 0
	d.HTTP = &http.Client{Transport: cardFlowTransport(func(r *http.Request) (*http.Response, error) {
		reply := func(body string) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/me/organizations":
			return reply(`{"data":[{"organization_id":"` + cardAddOrg + `","slug":"acme"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/"+cardAddOrg+"/billing/payment-methods":
			reads++
			return reply(lists[min(reads, len(lists))-1])
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		return nil, nil
	})}
	code := Execute(context.Background(), []string{"billing", "payment-methods", "add", "--org", cardAddOrg}, d)
	return code, out.String() + stderr.String(), opened, reads
}

func TestCardAddWaitsForCardAddedInConsole(t *testing.T) {
	for _, tt := range []struct {
		name     string
		terminal bool
		lists    []string
		reads    int
	}{
		{"added on first check", true, []string{cardListWithout, cardListWith}, 2},
		{"added after several checks", true, []string{cardListWithout, cardListWithout, cardListWithout, cardListWith}, 4},
		{"first card of the organization", true, []string{`{"data":[]}`, `{"data":[` + addedCard + `]}`}, 2},
		{"not a terminal", false, []string{cardListWithout, cardListWith}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, output, opened, reads := runCardAdd(t, tt.terminal, tt.lists...)
			if code != 0 || reads != tt.reads || !strings.Contains(output, cardAddPage) || !strings.Contains(output, "Card added: visa ****3155 (expires 3/2028)") {
				t.Fatal(code, reads, output)
			}
			if strings.Contains(output, "****4444") {
				t.Fatal("reported an existing card:", output)
			}
			if tt.terminal != (len(opened) == 1 && opened[0] == cardAddPage) || len(opened) > 1 {
				t.Fatal("browser opened only on a terminal:", opened)
			}
		})
	}
}

func TestCardAddTimesOutWithoutNewCard(t *testing.T) {
	// A removed card must not count as an added one.
	code, output, _, reads := runCardAdd(t, false, cardListWith, cardListWithout)
	if code != 1 || !strings.Contains(output, "No new card was added within 15 minutes.") || strings.Contains(output, "Card added") {
		t.Fatal(code, output)
	}
	if reads != 1+int(cardAddTimeout/(3*time.Second)) {
		t.Fatal("reads", reads)
	}
}

func TestCardAddRejectsCardDetailFlags(t *testing.T) {
	for _, flag := range []string{"--number", "--exp-month", "--exp-year", "--cvc"} {
		d, _, _ := testDeps(t)
		if code := Execute(context.Background(), []string{"billing", "payment-methods", "add", flag, "1"}, d); code != 2 {
			t.Fatal(flag, code)
		}
	}
}
