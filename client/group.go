package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// DataRecord sends a request to a route that wraps its object as
// {"data": {...}} and returns that object.
func (c *Client) DataRecord(ctx context.Context, method, path string, body any) (map[string]any, error) {
	result, err := c.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	fields, ok := result["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected a data object")
	}
	return decodeRecord(fields)
}

// DataList reads one page of a route that answers {"data": [...],
// "pagination": {...}}.
func (c *Client) DataList(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

// PostList sends a read that carries its input in a POST body, such as an
// email address kept out of URLs, and returns the listing it answers.
func (c *Client) PostList(ctx context.Context, path string, body any) (Page, error) {
	result, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return Page{}, err
	}
	return decodePage(result)
}
