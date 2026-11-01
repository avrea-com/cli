package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func billingRecord[T any](ctx context.Context, c *Client, method, path string, payload any) (Record[T], error) {
	body, err := c.Do(ctx, method, path, payload)
	if err != nil {
		return Record[T]{}, err
	}
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return Record[T]{}, errors.New("unexpected API response: expected billing data")
	}
	return decodeRecord[T](fields)
}
func (c *Client) BillingSummary(ctx context.Context, path string) (Record[wire.BillingSummaryResponse], error) {
	return billingRecord[wire.BillingSummaryResponse](ctx, c, http.MethodGet, path, nil)
}
func (c *Client) BillingSettings(ctx context.Context, path string) (Record[wire.BillingSettingsResponse], error) {
	return billingRecord[wire.BillingSettingsResponse](ctx, c, http.MethodGet, path, nil)
}
func (c *Client) Invoice(ctx context.Context, path string) (Record[wire.InvoiceResponse], error) {
	return billingRecord[wire.InvoiceResponse](ctx, c, http.MethodGet, path, nil)
}
func (c *Client) Invoices(ctx context.Context, path string, query url.Values) (Page[wire.InvoiceResponse], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.InvoiceResponse]{}, err
	}
	return decodePage[wire.InvoiceResponse](body)
}
func (c *Client) PaymentMethods(ctx context.Context, path string) (Page[wire.PaymentMethodResponse], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Page[wire.PaymentMethodResponse]{}, err
	}
	return decodePage[wire.PaymentMethodResponse](body)
}

func (c *Client) UpdateBillingSettings(ctx context.Context, path string, body wire.BillingSettingsRequest) (Record[wire.BillingSettingsResponse], error) {
	return billingRecord[wire.BillingSettingsResponse](ctx, c, http.MethodPut, path, body)
}
func (c *Client) SetDefaultPaymentMethod(ctx context.Context, path string) (Record[wire.PaymentMethodResponse], error) {
	return billingRecord[wire.PaymentMethodResponse](ctx, c, http.MethodPut, path, nil)
}
func (c *Client) RemovePaymentMethod(ctx context.Context, path string) error {
	_, err := c.Raw(ctx, http.MethodDelete, path, nil)
	return err
}
