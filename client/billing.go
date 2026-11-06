package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func billingRecord(ctx context.Context, c *Client, method, path string, payload any) (map[string]any, error) {
	body, err := c.Do(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected billing data")
	}
	return decodeRecord(fields)
}
func (c *Client) BillingSummary(ctx context.Context, path string) (map[string]any, error) {
	return billingRecord(ctx, c, http.MethodGet, path, nil)
}
func (c *Client) BillingSettings(ctx context.Context, path string) (map[string]any, error) {
	return billingRecord(ctx, c, http.MethodGet, path, nil)
}
func (c *Client) Invoice(ctx context.Context, path string) (map[string]any, error) {
	return billingRecord(ctx, c, http.MethodGet, path, nil)
}
func (c *Client) Invoices(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) PaymentMethods(ctx context.Context, path string) (Page, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) UpdateBillingSettings(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	return billingRecord(ctx, c, http.MethodPut, path, body)
}
func (c *Client) SetDefaultPaymentMethod(ctx context.Context, path string) (map[string]any, error) {
	return billingRecord(ctx, c, http.MethodPut, path, nil)
}
func (c *Client) RemovePaymentMethod(ctx context.Context, path string) error {
	_, err := c.Raw(ctx, http.MethodDelete, path, nil)
	return err
}
