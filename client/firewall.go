package client

import (
	"context"
	"encoding/json"
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

// FirewallPolicy is the portable, versioned document shared with the console.
// Raw JSON retains absent fields so the server supplies its schema defaults.
type FirewallPolicy struct {
	Version int              `json:"version"`
	Rules   []map[string]any `json:"rules"`
}

type FirewallImportPreview struct {
	Policy   FirewallPolicy `json:"policy"`
	Before   FirewallPolicy `json:"before"`
	After    FirewallPolicy `json:"after"`
	Revision string         `json:"revision"`
	Added    int            `json:"added"`
	Removed  int            `json:"removed"`
	Skipped  int            `json:"skipped"`
}

func (c *Client) ExportFirewall(ctx context.Context, path string) (FirewallPolicy, error) {
	data, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return FirewallPolicy{}, err
	}
	var policy FirewallPolicy
	err = json.Unmarshal(data, &policy)
	return policy, err
}

func (c *Client) PreviewFirewallImport(ctx context.Context, path, mode string, policy FirewallPolicy) (FirewallImportPreview, error) {
	data, err := c.Raw(ctx, http.MethodPost, path, map[string]any{"mode": mode, "policy": policy})
	if err != nil {
		return FirewallImportPreview{}, err
	}
	var preview FirewallImportPreview
	err = json.Unmarshal(data, &preview)
	return preview, err
}

func (c *Client) ImportFirewall(ctx context.Context, path, mode string, policy FirewallPolicy, revision string) error {
	_, err := c.Raw(ctx, http.MethodPost, path, map[string]any{"mode": mode, "policy": policy, "expected_revision": revision})
	return err
}
