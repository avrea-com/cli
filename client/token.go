package client

import (
	"context"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) AccessTokens(ctx context.Context, path string, query url.Values) (Page[wire.AccessTokenResponse], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.AccessTokenResponse]{}, err
	}
	return decodePage[wire.AccessTokenResponse](body)
}
func (c *Client) AccessToken(ctx context.Context, path string) (Record[wire.AccessTokenResponse], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.AccessTokenResponse]{}, err
	}
	return decodeRecord[wire.AccessTokenResponse](body)
}
