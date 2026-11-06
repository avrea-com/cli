package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type rawRequestContent struct {
	data             []byte
	contentType      string
	noTransportRetry bool
}

// RawContent uses the same authenticated, bounded transport for non-JSON bodies.
func (c *Client) RawContent(ctx context.Context, method, path string, content []byte, contentType string) ([]byte, error) {
	return c.Raw(ctx, method, path, rawRequestContent{data: content, contentType: contentType})
}
func samlRecord(body map[string]any) (map[string]any, error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected SAML configuration data")
	}
	return decodeRecord(fields)
}
func (c *Client) SAML(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return samlRecord(body)
}
func (c *Client) ConfigureSAML(ctx context.Context, path string, metadata []byte, query url.Values) (map[string]any, error) {
	raw, err := c.RawContent(ctx, http.MethodPost, path+"?"+query.Encode(), metadata, "application/xml")
	if err != nil {
		return nil, err
	}
	body, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	return samlRecord(body)
}
func (c *Client) EnforceSAML(ctx context.Context, path string, enforce bool) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPost, path, map[string]any{"enforce": enforce})
	if err != nil {
		return nil, err
	}
	return samlRecord(body)
}
