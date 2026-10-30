package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) FirewallRules(ctx context.Context, path string) (Page[wire.EgressRule], error) {
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page[wire.EgressRule]{}, err
	}
	return decodeArray[wire.EgressRule](raw)
}
func (c *Client) ResolvedFirewall(ctx context.Context, path string) (Record[wire.EgressFirewallPolicy], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.EgressFirewallPolicy]{}, err
	}
	return decodeRecord[wire.EgressFirewallPolicy](body)
}
func (c *Client) FirewallFlows(ctx context.Context, path string, query url.Values) (Record[wire.FlowSummarySearchResponse], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Record[wire.FlowSummarySearchResponse]{}, err
	}
	return decodeRecord[wire.FlowSummarySearchResponse](body)
}
func (c *Client) CreateFirewallRule(ctx context.Context, path string, body wire.EgressRuleCreate) (Record[wire.EgressRule], error) {
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return Record[wire.EgressRule]{}, err
	}
	return decodeRecord[wire.EgressRule](response)
}
func (c *Client) UpdateFirewallRule(ctx context.Context, path string, body wire.EgressRuleUpdate) (Record[wire.EgressRule], error) {
	response, err := c.Do(ctx, http.MethodPatch, path, body)
	if err != nil {
		return Record[wire.EgressRule]{}, err
	}
	return decodeRecord[wire.EgressRule](response)
}
func (c *Client) DeleteFirewallRule(ctx context.Context, path string) error {
	_, err := c.Raw(ctx, http.MethodDelete, path, nil)
	return err
}
func (c *Client) ReorderFirewallRules(ctx context.Context, path string, ids []string) error {
	_, err := c.Raw(ctx, http.MethodPost, path, wire.ReorderRequest{RuleIds: ids})
	return err
}
