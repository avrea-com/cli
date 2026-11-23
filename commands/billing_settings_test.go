package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestBillingSettingsPublicOutput(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, legacy := range []bool{false, true} {
			for _, asJSON := range []bool{false, true} {
				t.Run(fmt.Sprintf("update=%t/legacy=%t/json=%t", update, legacy, asJSON), func(t *testing.T) {
					d, out, stderr := testDeps(t)
					d.LoadConfig = func() (config.Config, error) {
						return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
					}
					args := []string{"billing", "settings"}
					method := http.MethodGet
					if update {
						args = []string{"billing", "update-settings", "--tax-id", "FI123"}
						method = http.MethodPut
					}
					if asJSON {
						args = append(args, "--json", "*")
					}
					public := map[string]any{"billing_emails": []any{"billing@example.com"}, "tax_id": "FI123", "billing_address": map[string]any{"line1": "1 Main St", "city": "Helsinki", "country": "FI"}}
					calls := 0
					d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.Method != method || r.URL.Path != "/orgs/"+inspectionOrg+"/billing/settings" {
							t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
						}
						if update {
							var body map[string]any
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, map[string]any{"tax_id": "FI123"}) {
								t.Fatalf("update body: %v, error: %v", body, err)
							}
						}
						row := map[string]any{}
						for key, value := range public {
							row[key] = value
						}
						if legacy {
							row["stripe_customer_id"] = "cus_private"
							row["metronome_customer_id"] = "private-customer"
						}
						body, err := json.Marshal(map[string]any{"data": row})
						if err != nil {
							t.Fatal(err)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
					})}
					if code := Execute(t.Context(), args, d); code != 0 || calls != 1 {
						t.Fatal(code, calls, stderr.String())
					}
					if asJSON {
						var got map[string]any
						if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, public) {
							t.Fatalf("public settings: %v, error: %v", got, err)
						}
					} else {
						text := out.String()
						if !strings.Contains(text, "billing@example.com") || !strings.Contains(text, "FI123") || !strings.Contains(text, "1 Main St, Helsinki, FI") {
							t.Fatal(text)
						}
						for _, private := range []string{"Stripe", "Metronome", "cus_private", "private-customer"} {
							if strings.Contains(text, private) {
								t.Fatalf("private billing field in output: %s", text)
							}
						}
					}
				})
			}
		}
	}
}

func TestBillingSettingsFieldDiscovery(t *testing.T) {
	for _, command := range []string{"settings", "update-settings"} {
		for _, flags := range [][]string{{"--help"}, {"--json", "?"}} {
			t.Run(command+strings.Join(flags, " "), func(t *testing.T) {
				d, out, stderr := testDeps(t)
				args := append([]string{"billing", command}, flags...)
				if code := Execute(t.Context(), args, d); code != 0 {
					t.Fatal(code, stderr.String())
				}
				text := out.String() + stderr.String()
				// Help and "--json ?" list exactly the fields the command outputs.
				for _, field := range billingSettingsFields {
					if !strings.Contains(text, field) {
						t.Fatalf("%s missing from\n%s", field, text)
					}
				}
				if strings.Contains(text, "stripe_customer_id") || strings.Contains(text, "metronome_customer_id") {
					t.Fatal(text)
				}
			})
		}
	}
}
