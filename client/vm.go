package client

import (
	"context"
	"errors"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

func vmData[T any](body map[string]any) (Record[T], error) {
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return Record[T]{}, errors.New("unexpected API response: expected VM data")
	}
	return decodeRecord[T](fields)
}
func (c *Client) VMs(ctx context.Context, path string, query url.Values) (Page[wire.CustomerVm], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Page[wire.CustomerVm]{}, err
	}
	return decodePage[wire.CustomerVm](body)
}
func (c *Client) VM(ctx context.Context, path string) (Record[wire.CustomerVmDetail], error) {
	body, err := c.Get(ctx, path, nil)
	if err != nil {
		return Record[wire.CustomerVmDetail]{}, err
	}
	return vmData[wire.CustomerVmDetail](body)
}
func (c *Client) VMUsage(ctx context.Context, path string, query url.Values) (Record[wire.CustomerVmUsageReport], error) {
	body, err := c.Get(ctx, path, query)
	if err != nil {
		return Record[wire.CustomerVmUsageReport]{}, err
	}
	return vmData[wire.CustomerVmUsageReport](body)
}
