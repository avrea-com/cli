package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) MirrorSession(ctx context.Context, repository string) (Record[wire.MirrorSession], error) {
	parts := strings.Split(repository, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	fields, err := c.Get(ctx, "/repos/"+strings.Join(parts, "/")+"/mirror/session", nil)
	if err != nil {
		return Record[wire.MirrorSession]{}, err
	}
	return decodeRecord[wire.MirrorSession](fields)
}
func (c *Client) GitToken(ctx context.Context, repository string) (Record[wire.GitTokenResponse], error) {
	fields, err := c.Do(ctx, http.MethodPost, "/tokens/git", map[string]any{"repos": []string{repository}})
	if err != nil {
		return Record[wire.GitTokenResponse]{}, err
	}
	return decodeRecord[wire.GitTokenResponse](fields)
}
