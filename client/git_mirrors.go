package client

import (
	"bytes"
	"context"
	"errors"
	"net/url"
)

func (c *Client) GitMirror(ctx context.Context, method, path string, body any) (map[string]any, error) {
	raw, err := c.Raw(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return decodeRecord(fields)
}

func (c *Client) GitClusters(ctx context.Context, org string, query url.Values) (Page, error) {
	fields, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/git-clusters", query)
	if err != nil {
		return Page{}, err
	}
	rows, ok := fields["data"].([]any)
	if !ok {
		return Page{}, errors.New("unexpected response while listing mirror locations")
	}
	for _, row := range rows {
		if _, ok := row.(map[string]any); !ok {
			return Page{}, errors.New("unexpected response while listing mirror locations")
		}
	}
	pagination, ok := fields["pagination"].(map[string]any)
	next, exists := pagination["next_cursor"]
	if !ok || !exists {
		return Page{}, errors.New("unexpected pagination response while listing mirror locations")
	}
	if next != nil {
		if cursor, ok := next.(string); !ok || cursor == "" {
			return Page{}, errors.New("unexpected pagination response while listing mirror locations")
		}
	}
	return decodePage(fields)
}
