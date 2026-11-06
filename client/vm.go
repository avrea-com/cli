package client

import (
	"context"
	"errors"
	"net/url"
)

func vmData(body map[string]any) (map[string]any, error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected VM data")
	}
	return decodeRecord(fields)
}
func (c *Client) VMs(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) VM(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return vmData(body)
}
func (c *Client) VMUsage(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	return vmData(body)
}

func (c *Client) VMCapabilities(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return vmData(body)
}

func (c *Client) VMMetrics(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	return vmData(body)
}

// vmEnvelope validates mutation data while preserving the original envelope.
func vmEnvelope(body map[string]any) (map[string]any, error) {
	if body["data"] != nil {
		if _, err := vmData(body); err != nil {
			return nil, err
		}
	}
	return body, nil
}
