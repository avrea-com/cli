package client

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) FirewallRules(ctx context.Context, path string) (Page, error) {
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodeArray(raw)
}
func (c *Client) ResolvedFirewall(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) FirewallFlows(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) CreateFirewallRule(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return decodeRecord(response)
}
func (c *Client) UpdateFirewallRule(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	response, err := c.Do(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	return decodeRecord(response)
}
func (c *Client) DeleteFirewallRule(ctx context.Context, path string) error {
	_, err := c.Raw(ctx, http.MethodDelete, path, nil)
	return err
}
func (c *Client) ReorderFirewallRules(ctx context.Context, path string, ids []string) error {
	_, err := c.Raw(ctx, http.MethodPost, path, map[string]any{"rule_ids": ids})
	return err
}
