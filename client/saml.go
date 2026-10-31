package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

type rawRequestContent struct {
	data        []byte
	contentType string
}

// RawContent uses the same authenticated, bounded transport for non-JSON bodies.
func (c *Client) RawContent(ctx context.Context, method, path string, content []byte, contentType string) ([]byte, error) {
	return c.Raw(ctx, method, path, rawRequestContent{content, contentType})
}
func samlRecord(body map[string]any) (Record[wire.SamlConfigResponse], error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return Record[wire.SamlConfigResponse]{}, errors.New("unexpected API response: expected SAML configuration data")
	}
	return decodeRecord[wire.SamlConfigResponse](fields)
}
func (c *Client) SAML(ctx context.Context, path string) (Record[wire.SamlConfigResponse], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.SamlConfigResponse]{}, err
	}
	return samlRecord(body)
}
func (c *Client) ConfigureSAML(ctx context.Context, path string, metadata []byte, query url.Values) (Record[wire.SamlConfigResponse], error) {
	raw, err := c.RawContent(ctx, http.MethodPost, path+"?"+query.Encode(), metadata, "application/xml")
	if err != nil {
		return Record[wire.SamlConfigResponse]{}, err
	}
	body, err := decodeObject(raw)
	if err != nil {
		return Record[wire.SamlConfigResponse]{}, err
	}
	return samlRecord(body)
}
func (c *Client) EnforceSAML(ctx context.Context, path string, enforce bool) (Record[wire.SamlConfigResponse], error) {
	body, err := c.Do(ctx, http.MethodPost, path, map[string]any{"enforce": enforce})
	if err != nil {
		return Record[wire.SamlConfigResponse]{}, err
	}
	return samlRecord(body)
}
