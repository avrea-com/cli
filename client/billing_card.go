package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) StripeKey(ctx context.Context, path string) (Record[wire.StripeKeyResponse], error) {
	return billingRecord[wire.StripeKeyResponse](ctx, c, http.MethodGet, path, nil)
}
func (c *Client) SetupIntent(ctx context.Context, path string) (Record[wire.SetupIntentResponse], error) {
	return billingRecord[wire.SetupIntentResponse](ctx, c, http.MethodPost, path, nil)
}
func (c *Client) AttachPaymentMethod(ctx context.Context, path, id string) (Record[wire.PaymentMethodResponse], error) {
	return billingRecord[wire.PaymentMethodResponse](ctx, c, http.MethodPost, path, wire.AttachPaymentMethodRequest{StripePaymentMethodId: id})
}

var stripeIntentID = regexp.MustCompile("^seti_[A-Za-z0-9]+$")

// StripeCard contains input sent only to Stripe. It must never be logged.
type StripeCard struct {
	Number, CVC string
	Month, Year int
}
type StripeError struct{ Message string }

func (e *StripeError) Error() string { return e.Message }

// ConfirmCard uses a fixed Stripe origin and deliberately bypasses API tracing,
// authentication and cookies. Its transport is injectable for isolated tests.
func (c *Client) ConfirmCard(ctx context.Context, key, secret string, card StripeCard) (string, error) {
	at := strings.LastIndex(secret, "_secret_")
	if at < 0 || at+len("_secret_") == len(secret) {
		return "", &StripeError{"Invalid SetupIntent client secret format."}
	}
	id := secret[:at]
	if !stripeIntentID.MatchString(id) {
		return "", &StripeError{"Invalid SetupIntent client secret format."}
	}
	form := url.Values{
		"payment_method_data[type]":            {"card"},
		"payment_method_data[card][number]":    {card.Number},
		"payment_method_data[card][exp_month]": {strconv.Itoa(card.Month)},
		"payment_method_data[card][exp_year]":  {strconv.Itoa(card.Year)},
		"payment_method_data[card][cvc]":       {card.CVC},
		"client_secret":                        {secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com/v1/setup_intents/"+id+"/confirm", strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("could not construct Stripe request")
	}
	req.SetBasicAuth(key, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	transport := *c.HTTP
	transport.Timeout = 30 * time.Second
	transport.Jar = nil
	transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &StripeError{"Failed to connect to Stripe: connection failed or timed out"}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return "", &StripeError{"Failed to read Stripe response."}
	}
	var body map[string]any
	decodeErr := json.Unmarshal(raw, &body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := "Stripe rejected the card details."
		failure, _ := body["error"].(map[string]any)
		detail, _ := failure["message"].(string)
		for _, sensitive := range []string{card.Number, card.CVC, key, secret} {
			if sensitive != "" {
				detail = strings.ReplaceAll(detail, sensitive, "[redacted]")
			}
		}
		if detail != "" {
			message += " (" + detail + ")"
		}
		return "", &StripeError{message}
	}
	if decodeErr != nil {
		return "", &StripeError{"Invalid response from Stripe."}
	}
	pm, _ := body["payment_method"].(string)
	if pm == "" {
		return "", &StripeError{"No payment method returned from Stripe."}
	}
	return pm, nil
}
