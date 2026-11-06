package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Page retains the response envelope, record maps and pagination cursor.
// JSON output and terminal rendering share the same decoded response values.
type Page struct {
	Fields     map[string]any
	Items      []map[string]any
	NextCursor string
}

func (c *Client) Get(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.Do(ctx, http.MethodGet, path, nil)
}

func decodePage(body map[string]any) (Page, error) {
	page := Page{Fields: body, Items: []map[string]any{}}
	raw, ok := body["data"].([]any)
	if !ok && body["data"] != nil {
		return page, errors.New("unexpected API response: expected a data array")
	}
	for index, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			return page, errors.New("unexpected API response: expected object in data array")
		}
		record, err := decodeRecord(fields)
		if err != nil {
			return page, fmt.Errorf("row %d: %w", index+1, err)
		}
		page.Items = append(page.Items, record)
	}
	if value := body["pagination"]; value != nil {
		pagination, ok := value.(map[string]any)
		if !ok {
			return page, errors.New("unexpected API response: expected pagination object")
		}
		if value := pagination["next_cursor"]; value != nil {
			cursor, ok := value.(string)
			if !ok {
				return page, errors.New("unexpected API response: expected string pagination cursor")
			}
			page.NextCursor = cursor
		}
	}
	return page, nil
}

func (c *Client) Organizations(ctx context.Context) (Page, error) {
	body, err := c.Get(ctx, "/users/me/organizations", nil)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) Repositories(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) ResolveRepository(ctx context.Context, org string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/resolve", query)
	if err != nil {
		return nil, err
	}
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected repository data")
	}
	return decodeRecord(fields)
}

func (c *Client) Runs(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

// RunByReference accepts an already parsed Avrea ID or decimal platform ID.
// It never follows the URL supplied by a user or sends credentials to it.
func (c *Client) RunByReference(ctx context.Context, org, id string, platform bool, query url.Values) (map[string]any, error) {
	path := "/orgs/" + url.PathEscape(org) + "/workflow-runs/"
	if platform {
		path += "by-platform-id/"
	}
	body, err := c.Get(ctx, path+url.PathEscape(id), query)
	if err != nil {
		return nil, err
	}
	fields := body
	if wrapped, ok := body["data"].(map[string]any); ok {
		fields = wrapped
	}
	if id, ok := fields["run_id"].(string); !ok || id == "" {
		return nil, errors.New("avrea returned an invalid workflow-run response")
	}
	return decodeRecord(fields)
}

// SearchLogs retains raw values for display while validating the wire record.
func (c *Client) SearchLogs(ctx context.Context, payload map[string]any) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPost, "/logs/search", payload)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

func (c *Client) Jobs(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) Job(ctx context.Context, org, id string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs/"+url.PathEscape(id), query)
	if err != nil {
		return nil, err
	}
	if wrapped, ok := body["data"].(map[string]any); ok {
		body = wrapped
	}
	return decodeRecord(body)
}

func (c *Client) SSHForwarding(ctx context.Context, id string) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPost, "/vms/ssh-forwarding", map[string]any{"job_id": id})
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

func (c *Client) CancelRun(ctx context.Context, org, id string) (map[string]any, error) {
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}

func (c *Client) Rerun(ctx context.Context, org, id string, failed bool) error {
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/rerun", map[string]any{"failed_only": failed})
	if err != nil {
		return err
	}
	fields, _ := body["data"].(map[string]any)
	_, err = decodeRecord(fields)
	return err
}

func (c *Client) WorkflowAggregates(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs/aggregate", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) RepositoryWorkflows(ctx context.Context, org, repo string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/workflows", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) Repository(ctx context.Context, org, id string) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}
func (c *Client) DispatchWorkflow(ctx context.Context, org, repo, file, ref string, inputs map[string]string) (map[string]any, error) {
	request := map[string]any{"workflow": file, "ref": ref}
	if len(inputs) > 0 {
		request["inputs"] = inputs
	}
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/dispatch-workflow", request)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}

func (c *Client) JobMetrics(ctx context.Context, org, id, source string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs/"+url.PathEscape(id)+"/metrics/"+url.PathEscape(source), query)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

func (c *Client) RunJobs(ctx context.Context, org, id string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/jobs", query)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) RunMetrics(ctx context.Context, org, id string, query url.Values) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/metrics", query)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}

func (c *Client) FeatureFlag(ctx context.Context, org, key string) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/feature-flags/"+url.PathEscape(key), nil)
	if err != nil {
		return nil, err
	}
	return decodeRecord(body)
}
func (c *Client) PullRequests(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/pull-requests", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}

func (c *Client) JobAggregates(ctx context.Context, org string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs/aggregate", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
func (c *Client) CacheUsage(ctx context.Context, org, repo string) (map[string]any, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/cache", nil)
	if err != nil {
		return nil, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord(fields)
}

func (c *Client) CacheEntries(ctx context.Context, org, repo string, query url.Values) (Page, error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/cache/entries", query)
	if err != nil {
		return Page{}, err
	}
	return decodePage(body)
}
