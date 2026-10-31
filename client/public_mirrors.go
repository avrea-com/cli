package client

import (
	"bytes"
	"context"
	"net/http"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) PublicMirror(ctx context.Context, path string) (Record[wire.LegacyGitHubPublicMirrorCatalogResource], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.LegacyGitHubPublicMirrorCatalogResource]{}, err
	}
	return decodeRecord[wire.LegacyGitHubPublicMirrorCatalogResource](body)
}
func (c *Client) PublicMirrorRequests(ctx context.Context, path string) (Page[wire.PublicMirrorRequestResource], error) {
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page[wire.PublicMirrorRequestResource]{}, err
	}
	return decodeArray[wire.PublicMirrorRequestResource](raw)
}
func (c *Client) PublicMirrorRequest(ctx context.Context, method, path string, body any, fallbackID string) (Record[wire.PublicMirrorRequestResource], error) {
	raw, err := c.Raw(ctx, method, path, body)
	if err != nil {
		return Record[wire.PublicMirrorRequestResource]{}, err
	}
	var fields map[string]any
	if fallbackID != "" && (len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))) {
		fields = map[string]any{"request_id": fallbackID}
	} else {
		fields, err = decodeObject(raw)
		if err != nil {
			return Record[wire.PublicMirrorRequestResource]{}, err
		}
		if len(fields) == 0 && fallbackID != "" {
			fields["request_id"] = fallbackID
		}
	}
	return decodeRecord[wire.PublicMirrorRequestResource](fields)
}
