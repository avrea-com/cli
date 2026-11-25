package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
)

func builderData(body map[string]any) (map[string]any, error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected builder session data")
	}
	return decodeRecord(fields)
}

func (c *Client) BuilderSessions(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) BuilderSession(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return builderData(body)
}

func (c *Client) CreateBuilderSession(ctx context.Context, path string, request map[string]any) (map[string]any, error) {
	response, err := c.Do(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, err
	}
	return builderData(response)
}

// DeleteBuilderSession returns the response envelope, or nil when the API
// sends no body.
func (c *Client) DeleteBuilderSession(ctx context.Context, path string) (map[string]any, error) {
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
	if body["data"] != nil {
		if _, err = builderData(body); err != nil {
			return nil, err
		}
	}
	return body, nil
}
