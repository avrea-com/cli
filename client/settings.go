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

// Settings endpoints return bare arrays, unlike the paginated inspection APIs.
func decodeArray(data []byte) (Page, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil {
		return Page{}, errors.New("unexpected API response: expected a record array")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Page{}, errors.New("unexpected API response: trailing JSON")
	}
	page := Page{Items: []map[string]any{}}
	for _, row := range rows {
		record, err := decodeRecord(row)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, record)
	}
	return page, nil
}
func (c *Client) Settings(ctx context.Context, path string, query url.Values) (Page, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodeArray(raw)
}
func (c *Client) SettingsSchema(ctx context.Context, query url.Values) (Page, error) {
	path := "/settings/schema"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	raw, err := c.Raw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Page{}, err
	}
	return decodeArray(raw)
}
func (c *Client) SetSetting(ctx context.Context, path, key string, value any) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPut, path, map[string]any{"key": key, "value": value})
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
