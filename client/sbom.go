package client

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) SBOMs(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) SBOM(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

func (c *Client) GenerateSBOM(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	result, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return decodeRecord(result)
}
func (c *Client) SBOMTaskState(ctx context.Context, path string) (map[string]any, error) {
	result, err := c.Get(ctx, path, url.Values{"scope": {"sbom"}})
	if err != nil {
		return nil, err
	}
	return decodeRecord(result)
}
