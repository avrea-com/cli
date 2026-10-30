package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

// Settings endpoints return bare arrays, unlike the paginated inspection APIs.
func decodeArray[T any](data []byte) (Page[T], error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil {
		return Page[T]{}, errors.New("unexpected API response: expected a record array")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Page[T]{}, errors.New("unexpected API response: trailing JSON")
	}
	page := Page[T]{Items: []Record[T]{}}
	for _, row := range rows {
		record, err := decodeRecord[T](row)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, record)
	}
	return page, nil
}
func (c *Client) Settings(ctx context.Context, path string, query url.Values) (Page[wire.SettingResponse], error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page[wire.SettingResponse]{}, err
	}
	return decodeArray[wire.SettingResponse](raw)
}
func (c *Client) SettingsSchema(ctx context.Context, query url.Values) (Page[wire.SettingSchemaResponse], error) {
	path := "/settings/schema"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page[wire.SettingSchemaResponse]{}, err
	}
	return decodeArray[wire.SettingSchemaResponse](raw)
}
func (c *Client) SetSetting(ctx context.Context, path, key string, value any) (Record[wire.SettingResponse], error) {
	body, err := c.Do(ctx, http.MethodPut, path, map[string]any{"key": key, "value": value})
	if err != nil {
		return Record[wire.SettingResponse]{}, err
	}
	return decodeRecord[wire.SettingResponse](body)
}
