package client

import (
	"context"
	"net/http"
)

func (c *Client) CreateVM(ctx context.Context, path string, request map[string]any, rules *[]any, bootstrap map[string]any) (map[string]any, error) {
	body := vmRequestWithRules(request, rules)
	if bootstrap != nil {
		body["bootstrap"] = bootstrap
	}
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return vmData(response)
}
