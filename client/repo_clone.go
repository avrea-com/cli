package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) MirrorSession(ctx context.Context, repository string) (map[string]any, error) {
	parts := strings.Split(repository, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	fields, err := c.Get(ctx, "/repos/"+strings.Join(parts, "/")+"/mirror/session", nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(fields)
}
func (c *Client) GitToken(ctx context.Context, repository string) (map[string]any, error) {
	fields, err := c.Do(ctx, http.MethodPost, "/tokens/git", map[string]any{"repos": []string{repository}})
	if err != nil {
		return nil, err
	}
	return decodeRecord(fields)
}
