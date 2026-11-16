package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

func (c *Client) AccessTokens(ctx context.Context, path string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) AccessToken(ctx context.Context, path string) (map[string]any, error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

// ServiceTokenIdentity reads the presented service token's own identity. The
// API answers only a service token; other credentials receive 403.
func (c *Client) ServiceTokenIdentity(ctx context.Context) (map[string]any, error) {
	return c.AccessToken(ctx, "/service-tokens/self")
}

// MintAccessToken keeps the successful raw JSON until the one-time credential
// is delivered. An unrelated malformed summary field must not discard it.
func (c *Client) MintAccessToken(ctx context.Context, path string, body map[string]any) (any, error) {
	raw, err := c.Raw(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid token creation response")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid token creation response")
	}
	return value, nil
}
