package client

import (
	"bytes"
	"context"
	"maps"
	"net/http"
)

func (c *Client) TransitionVM(ctx context.Context, path string, body any) (map[string]any, error) {
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return vmEnvelope(response)
}
func (c *Client) DeleteVM(ctx context.Context, path string) (map[string]any, error) {
	raw, err := c.Raw(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	body, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return vmEnvelope(body)
}

// vmRequestWithRules preserves explicit empty rule lists and unknown properties.
func vmRequestWithRules(body map[string]any, rules *[]any) map[string]any {
	body = maps.Clone(body)
	if body == nil {
		body = map[string]any{}
	}
	if rules != nil {
		body["egress_rules"] = *rules
	}
	return body
}
func (c *Client) UpdateVM(ctx context.Context, path string, request map[string]any, rules *[]any) (map[string]any, error) {
	body := vmRequestWithRules(request, rules)
	raw, err := c.Raw(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	response, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return vmEnvelope(response)
}
