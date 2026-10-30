package client

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

type cardTransport func(*http.Request) (*http.Response, error)

func (f cardTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStripeFailureDoesNotLeakCardOrAPISecrets(t *testing.T) {
	for _, mode := range []string{"transport", "rejection", "redirect", "large"} {
		t.Run(mode, func(t *testing.T) {
			card := StripeCard{Number: "4242424242424242", CVC: "987", Month: 3, Year: 2028}
			key, secret := "pk_test_fixture", "seti_fixture_secret_synthetic"
			var trace bytes.Buffer
			calls := 0
			jar, _ := cookiejar.New(nil)
			stripe, _ := url.Parse("https://api.stripe.com")
			jar.SetCookies(stripe, []*http.Cookie{{Name: "private", Value: "cookie"}})
			c := New(config.Config{Host: "https://api.example.test", Token: "api-private"}, &http.Client{Jar: jar, Transport: cardTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				user, password, ok := r.BasicAuth()
				if r.URL.String() != "https://api.stripe.com/v1/setup_intents/seti_fixture/confirm" || !ok || user != key || password != "" || r.Header.Get("Cookie") != "" {
					t.Fatal(r.URL, user, password, r.Header)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("client_secret") != secret || r.Form.Get("payment_method_data[card][number]") != card.Number || r.Form.Get("payment_method_data[card][cvc]") != card.CVC {
					t.Fatal("wrong form")
				}
				if mode == "transport" {
					return nil, errors.New(card.Number + " " + card.CVC + " " + secret)
				}
				status := 400
				body := "{\"error\":{\"message\":\"" + card.Number + " " + card.CVC + " " + key + " " + secret + "\"}}"
				headers := http.Header{}
				if mode == "redirect" {
					status = 302
					headers.Set("Location", "https://other.example.test/collect")
				}
				if mode == "large" {
					status = 200
					body = strings.Repeat("x", (1<<20)+1)
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}, "test")
			c.Trace = &trace
			c.Timings = &trace
			_, err := c.ConfirmCard(t.Context(), key, secret, card)
			if err == nil || calls != 1 || trace.Len() != 0 {
				t.Fatal(err, calls, trace.String())
			}
			for _, sensitive := range []string{card.Number, card.CVC, key, secret, "api-private"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatal("secret in error")
				}
			}
		})
	}
}
func TestInvalidStripeIntentCannotChangeRequestPath(t *testing.T) {
	calls := 0
	c := New(config.Config{}, &http.Client{Transport: cardTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })}, "test")
	for _, secret := range []string{"seti_../other_secret_value", "seti_x?bad_secret_value", "seti_x_secret_", "seti_/bad_secret_value", "bad"} {
		if _, err := c.ConfirmCard(t.Context(), "key", secret, StripeCard{}); err == nil {
			t.Fatal("accepted malformed secret")
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
