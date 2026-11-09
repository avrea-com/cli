package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/google/uuid"
)

const feedbackTestID = "fdb-019a4770720070008000000000000001"
const feedbackTestOrg = "org-019a4770720070008000000000000002"
const feedbackTestKey = "019a4770720070008000000000000003"
const feedbackTestResponse = `{"id":"` + feedbackTestID + `","status":"queued","created_at":"2026-11-05T12:00:00Z"}`

func TestFeedbackSubmission(t *testing.T) {
	for _, tc := range []struct {
		name, message, kind, org string
		flags                    []string
	}{
		{"personal", "  Hello 世界  ", "feedback", "", nil},
		{"bug", "A bug", "bug", feedbackTestOrg, []string{"--bug", "--org", feedbackTestOrg, "--idempotency-key", feedbackTestKey}},
		{"slug", "Hello", "feedback", "Acme", []string{"--org", "Acme"}},
		{"status message", "status", "feedback", "", []string{"--"}},
		{"help message", "help", "feedback", "", []string{"--"}},
		{"typo message", "stauts", "feedback", "", []string{"--"}},
		{"ordinary single word", "thanks", "feedback", "", nil},
		{"sentence about status", "status is confusing", "feedback", "", nil},
		{"hyphenated key", "Hello", "feedback", "", []string{"--idempotency-key", "019a4770-7200-7000-8000-000000000003"}},
		{"uppercase key", "Hello", "feedback", "", []string{"--idempotency-key", "019A4770720070008000000000000003"}},
		{"dash message", "--broken", "feedback", "", []string{"--"}},
		{"unicode boundary", strings.Repeat("🙂", 10000), "feedback", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			sends, lookups := 0, 0
			var key string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-token" || !strings.HasPrefix(r.UserAgent(), "avr-cli/test-version (") {
					t.Error("missing authentication or user agent", r.Header)
				}
				if r.URL.Path == "/users/me/organizations" {
					lookups++
					_, _ = fmt.Fprintf(w, `{"data":[{"organization_id":%q,"slug":"acme","name":"Acme","role":"owner"}]}`, feedbackTestOrg)
					return
				}
				sends++
				if r.Method != http.MethodPost || r.URL.Path != "/feedback" {
					t.Error(r.Method, r.URL.Path)
				}
				key = r.Header.Get("Idempotency-Key")
				parsedKey, keyErr := uuid.Parse(key)
				if keyErr != nil || key != strings.ReplaceAll(parsedKey.String(), "-", "") || !strings.Contains(stderr.String(), key) {
					t.Error("key must be visible before sending", key, stderr)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				want := map[string]any{"message": strings.TrimSpace(tc.message), "kind": tc.kind}
				if tc.org != "" {
					want["organization_id"] = feedbackTestOrg
				}
				a, _ := json.Marshal(body)
				b, _ := json.Marshal(want)
				if string(a) != string(b) {
					t.Error(string(a), string(b))
				}
				w.WriteHeader(http.StatusAccepted)
				_, _ = fmt.Fprint(w, feedbackTestResponse)
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "private-token", Org: "unused-default"}, nil
			}
			d.Version = "test-version"
			args := append([]string{"feedback", "--json", "*"}, tc.flags...)
			args = append(args, tc.message)
			if code := Execute(t.Context(), args, d); code != 0 {
				t.Fatal(code, stderr.String())
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err, out)
			}
			if result["id"] != feedbackTestID || result["status"] != "queued" || result["idempotency_key"] != key || sends != 1 {
				t.Fatal(result, sends)
			}
			if (tc.name == "bug" || tc.name == "hyphenated key" || tc.name == "uppercase key") && key != feedbackTestKey || tc.name == "slug" && lookups != 1 || tc.name != "slug" && lookups != 0 {
				t.Fatal(key, lookups)
			}
			if strings.Contains(out.String()+stderr.String(), "private-token") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestFeedbackRejectsInvalidInputWithoutRequests(t *testing.T) {
	for _, args := range [][]string{
		{"feedback"}, {"feedback", ""}, {"feedback", " \n "}, {"feedback", "a", "b"},
		{"feedback", strings.Repeat("🙂", 10001)}, {"feedback", "ok", "--org", ""},
		{"feedback", "ok", "--org", "org-short"},
		{"feedback", "ok", "--idempotency-key", ""},
		{"feedback", "status", "bad"}, {"feedback", "status"},
		{"feedback", "ok", "--json", "unknown"}, {"feedback", "ok", "--jq", ".id"},
	} {
		t.Run(fmt.Sprint(args[:min(2, len(args))]), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				t.Fatal("invalid input reached config loading")
				return config.Config{}, nil
			}
			if code := Execute(t.Context(), args, d); code == 0 || out.Len() != 0 {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestFeedbackRequiresSignIn(t *testing.T) {
	for _, args := range [][]string{{"feedback", "hello"}, {"feedback", "status", feedbackTestID}} {
		d, out, stderr := testDeps(t)
		if code := Execute(t.Context(), args, d); code != 4 || out.Len() != 0 || !strings.Contains(stderr.String(), "avr auth login") {
			t.Fatal(code, out, stderr)
		}
	}
}

func TestFeedbackFailureGuidance(t *testing.T) {
	for _, status := range []int{401, 403, 409, 422, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"detail":"request rejected"}`)
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "private-token"}, nil }
			code := Execute(t.Context(), []string{"feedback", "hello", "--idempotency-key", feedbackTestKey}, d)
			if code == 0 || calls != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "Idempotency key: "+feedbackTestKey) {
				t.Fatal(code, calls, out, stderr)
			}
			retry := strings.Index(stderr.String(), "Retry the same message")
			wantRetry := status == 429 || status >= 500
			if (retry >= 0) != wantRetry {
				t.Fatal("incorrect retry advice", status, stderr)
			}
			if wantRetry && (!strings.Contains(stderr.String(), "Error:") || retry < strings.Index(stderr.String(), "Error:") || !strings.Contains(stderr.String(), "  Hint: Retry") || !strings.Contains(stderr.String(), "--idempotency-key "+feedbackTestKey)) {
				t.Fatal("retry guidance must follow the error and retain the key", stderr)
			}
			if status == 409 && (!strings.Contains(stderr.String(), "original content") || !strings.Contains(stderr.String(), "omit --idempotency-key")) {
				t.Fatal("conflict must explain how to use a new submission", stderr)
			}
			if status == 429 && !strings.Contains(stderr.String(), "Retry in 60s") {
				t.Fatal(stderr)
			}
		})
	}
}

func TestFeedbackLostResponseCanBeRetriedWithSameKey(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "private-token"}, nil
	}
	var key string
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		key = r.Header.Get("Idempotency-Key")
		return nil, errors.New("private transport details")
	})}
	if code := Execute(t.Context(), []string{"feedback", "hello"}, d); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--idempotency-key "+key) || strings.Contains(stderr.String(), "private") {
		t.Fatal(code, out, stderr)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != key {
			t.Error("retry changed key")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, feedbackTestResponse)
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "private-token"}, nil }
	d.HTTP = server.Client()
	if code := Execute(t.Context(), []string{"feedback", "hello", "--idempotency-key", key}, d); code != 0 {
		t.Fatal(code, stderr)
	}
}

func TestFeedbackStatus(t *testing.T) {
	for _, status := range []string{"queued", "sending", "delivered", "failed", "needs_attention", "future_status"} {
		for _, structured := range []bool{false, true} {
			t.Run(fmt.Sprint(status, structured), func(t *testing.T) {
				d, out, stderr := testDeps(t)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/feedback/"+feedbackTestID || r.Header.Get("Authorization") != "Bearer token" {
						t.Error(r.Method, r.URL.Path, r.Header)
					}
					_, _ = fmt.Fprint(w, strings.Replace(feedbackTestResponse, "queued", status, 1))
				}))
				defer server.Close()
				d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "token"}, nil }
				args := []string{"feedback", "status", feedbackTestID}
				if structured {
					args = append(args, "--json", "status", "--jq", ".status")
				}
				if code := Execute(t.Context(), args, d); code != 0 || !strings.Contains(out.String(), status) || stderr.Len() != 0 {
					t.Fatal(code, out, stderr)
				}
			})
		}
	}
}

func TestFeedbackHelpAndDiscoveryWithoutAuthentication(t *testing.T) {
	for _, args := range [][]string{{"feedback", "--help"}, {"feedback", "status", "--help"}, {"feedback", "--json", "?"}, {"feedback", "status", feedbackTestID, "--json", "?"}} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) { t.Fatal("help must not load credentials"); return config.Config{}, nil }
		if code := Execute(t.Context(), args, d); code != 0 || out.Len() == 0 {
			t.Fatal(code, out, stderr)
		}
	}
}

func TestFeedbackCompletionWithoutAuthentication(t *testing.T) {
	for _, tc := range []struct{ words, word, want string }{
		{"avr fee", "1", "feedback"},
		{"avr feedback ", "2", "status"},
		{"avr feedback --", "2", "--idempotency-key"},
		{"avr feedback status --", "3", "--json"},
	} {
		d, out, stderr := testDeps(t)
		env := map[string]string{"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": tc.words, "COMP_CWORD": tc.word}
		d.Getenv = func(key string) string { return env[key] }
		d.LoadConfig = func() (config.Config, error) { t.Fatal("completion loaded credentials"); return config.Config{}, nil }
		if code := Execute(t.Context(), nil, d); code != 0 || !strings.Contains(out.String(), tc.want) || stderr.Len() != 0 {
			t.Fatal(code, out, stderr)
		}
	}
}

func TestFeedbackStatusNotFound(t *testing.T) {
	d, out, stderr := testDeps(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"detail":"Feedback not found"}`)
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "token"}, nil }
	if code := Execute(t.Context(), []string{"feedback", "status", feedbackTestID}, d); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "HTTP 404") {
		t.Fatal(code, out, stderr)
	}
}

func TestFeedbackUnreadableAcceptanceKeepsRetryHint(t *testing.T) {
	for _, body := range []string{`not JSON`, `{}`, `{"id":null,"status":"queued","created_at":"2026-11-05T12:00:00Z"}`} {
		t.Run(body, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "token"}, nil }
			if code := Execute(t.Context(), []string{"feedback", "hello", "--idempotency-key", feedbackTestKey}, d); code != 1 || out.Len() != 0 {
				t.Fatal(code, out, stderr)
			}
			text := stderr.String()
			if strings.Index(text, "  Hint: Retry") <= strings.Index(text, "Error:") || !strings.Contains(text, "--idempotency-key "+feedbackTestKey) {
				t.Fatal("uncertain acceptance needs a safe retry hint after the error", stderr)
			}
		})
	}
}

func TestFeedbackSubmissionShowsStatusCommandOnlyInHumanOutput(t *testing.T) {
	for _, structured := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, feedbackTestResponse)
		}))
		d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "token"}, nil }
		args := []string{"feedback", "hello"}
		if structured {
			args = append(args, "--json", "*")
		}
		code := Execute(t.Context(), args, d)
		server.Close()
		if code != 0 || strings.Contains(out.String(), "avr feedback status "+feedbackTestID) == structured {
			t.Fatal(code, out, stderr)
		}
		if !strings.Contains(stderr.String(), "Idempotency key:") {
			t.Fatal("submission must retain its key", stderr)
		}
	}
}

func TestFeedbackStatusNamesMissingArgument(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		t.Fatal("missing ID must not load credentials")
		return config.Config{}, nil
	}
	if code := Execute(t.Context(), []string{"feedback", "status"}, d); code != 2 || !strings.Contains(stderr.String(), "Missing argument 'FEEDBACK_ID'") {
		t.Fatal(code, stderr)
	}
}

func TestFeedbackGuardsCommandLikeMessages(t *testing.T) {
	for _, message := range []string{"help", "HELP", "stauts", "Status", "stats"} {
		t.Run(message, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				t.Fatal("command-like message reached config")
				return config.Config{}, nil
			}
			if code := Execute(t.Context(), []string{"feedback", message}, d); code != 2 || !strings.Contains(stderr.String(), "--help") || !strings.Contains(stderr.String(), "-- MESSAGE") {
				t.Fatal(code, stderr)
			}
		})
	}
}

func TestFeedbackRejectsScopedTokensBeforeRequests(t *testing.T) {
	for _, args := range [][]string{{"feedback", "hello"}, {"feedback", "hello", "--org", "acme"}, {"feedback", "status", feedbackTestID}} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "avs_test"}, nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			t.Error("scoped token reached API")
			return nil, errors.New("unexpected request")
		})}
		if code := Execute(t.Context(), args, d); code != 4 || out.Len() != 0 || !strings.Contains(stderr.String(), "AVR_TOKEN") || !strings.Contains(stderr.String(), "avr auth login") {
			t.Fatal(code, out, stderr)
		}
	}
}

func TestFeedbackCompletionExcludesInvalidContinuations(t *testing.T) {
	for _, tc := range []struct{ words, word, forbidden, wanted string }{
		{`avr feedback "slow cache" `, "3", "plain,status", ""},
		{"avr feedback --bug ", "3", "plain,status", ""},
		{"avr feedback --org acme ", "4", "plain,status", ""},
		{"avr feedback --idempotency-key " + feedbackTestKey + " ", "4", "plain,status", ""},
		{"avr feedback hello --bug --b", "4", "plain,--bug", ""},
		{"avr feedback hello --bug --j", "4", "plain,--bug", "plain,--json"},
		{"avr feedback -- status ", "4", "plain,status", ""},
		{"avr feedback -- --b", "3", "plain,--bug", ""},
		{"avr feedback --json '*' ", "4", "plain,--bug", "plain,status"},
	} {
		t.Run(tc.words, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			env := map[string]string{"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": tc.words, "COMP_CWORD": tc.word}
			d.Getenv = func(key string) string { return env[key] }
			d.LoadConfig = func() (config.Config, error) { t.Fatal("completion loaded config"); return config.Config{}, nil }
			if code := Execute(t.Context(), nil, d); code != 0 || strings.Contains(out.String(), tc.forbidden) || (tc.wanted != "" && !strings.Contains(out.String(), tc.wanted)) {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestFeedbackHelpShowsAlternativeInvocations(t *testing.T) {
	d, out, stderr := testDeps(t)
	if code := Execute(t.Context(), []string{"feedback", "--help"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if !strings.Contains(out.String(), "[OPTIONS] MESSAGE | status FEEDBACK_ID") || strings.Contains(out.String(), "COMMAND [ARGS]") {
		t.Fatal(out)
	}
}
