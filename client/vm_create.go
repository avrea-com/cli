package client

import (
	"context"
	"net/http"

	"github.com/avrea-com/cli/client/wire"
)

func (c *Client) CreateVM(ctx context.Context, path string, request wire.CustomerVmCreateRequest, rules *[]any, bootstrap *wire.ClaudeCodeBootstrap) (Record[wire.CustomerVmWithPassword], error) {
	body, err := vmRequestWithRules(request, rules)
	if err != nil {
		return Record[wire.CustomerVmWithPassword]{}, err
	}
	// The generated optional union has no exported constructor. Keep the concrete
	// bootstrap model typed while serializing it through the request envelope.
	if bootstrap != nil {
		body["bootstrap"] = bootstrap
	}
	response, err := c.Do(ctx, http.MethodPost, path, body)
	if err != nil {
		return Record[wire.CustomerVmWithPassword]{}, err
	}
	return vmData[wire.CustomerVmWithPassword](response)
}
