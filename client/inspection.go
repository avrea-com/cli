package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/avrea-com/cli/client/wire"
)

// Keep generated wire models behind the API client. Raw fields are retained for
// JSON projection: re-marshaling typed models would alter timestamps, omitted
// values, and unknown numeric fields.
type Organization = wire.OrganizationListItem
type Repository = wire.OrganizationRepositoryResponse
type Run = wire.WorkflowRunResponse
type Job = wire.JobResponse

type Record[T any] struct {
	Value  T
	Fields map[string]any
}

type Page[T any] struct {
	Items      []Record[T]
	NextCursor string
}

func (c *Client) Get(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.Do(ctx, http.MethodGet, path, nil)
}

func decodeRecord[T any](fields map[string]any) (Record[T], error) {
	var value T
	data, err := json.Marshal(fields)
	if err != nil {
		return Record[T]{}, errors.New("unexpected API response: invalid record")
	}
	if err = json.Unmarshal(data, &value); err != nil {
		return Record[T]{}, errors.New("unexpected API response: invalid record fields")
	}
	return Record[T]{Value: value, Fields: fields}, nil
}

func decodePage[T any](body map[string]any) (Page[T], error) {
	page := Page[T]{Items: []Record[T]{}}
	raw, ok := body["data"].([]any)
	if !ok && body["data"] != nil {
		return page, errors.New("unexpected API response: expected a data array")
	}
	for _, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			return page, errors.New("unexpected API response: expected object in data array")
		}
		record, err := decodeRecord[T](fields)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, record)
	}
	if pagination, ok := body["pagination"].(map[string]any); ok {
		page.NextCursor, _ = pagination["next_cursor"].(string)
	}
	return page, nil
}

func (c *Client) Organizations(ctx context.Context) (Page[Organization], error) {
	body, err := c.Get(ctx, "/users/me/organizations", nil)
	if err != nil {
		return Page[Organization]{}, err
	}
	return decodePage[Organization](body)
}

func (c *Client) Repositories(ctx context.Context, org string, query url.Values) (Page[Repository], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos", query)
	if err != nil {
		return Page[Repository]{}, err
	}
	return decodePage[Repository](body)
}

func (c *Client) ResolveRepository(ctx context.Context, org string, query url.Values) (Record[Repository], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/resolve", query)
	if err != nil {
		return Record[Repository]{}, err
	}
	fields, ok := body["data"].(map[string]any)
	if !ok {
		return Record[Repository]{}, errors.New("unexpected API response: expected repository data")
	}
	return decodeRecord[Repository](fields)
}

func (c *Client) Runs(ctx context.Context, org string, query url.Values) (Page[Run], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs", query)
	if err != nil {
		return Page[Run]{}, err
	}
	return decodePage[Run](body)
}

// RunByReference accepts an already parsed Avrea ID or decimal platform ID.
// It never follows the URL supplied by a user or sends credentials to it.
func (c *Client) RunByReference(ctx context.Context, org, id string, platform bool, query url.Values) (Record[Run], error) {
	path := "/orgs/" + url.PathEscape(org) + "/workflow-runs/"
	if platform {
		path += "by-platform-id/"
	}
	body, err := c.Get(ctx, path+url.PathEscape(id), query)
	if err != nil {
		return Record[Run]{}, err
	}
	fields := body
	if wrapped, ok := body["data"].(map[string]any); ok {
		fields = wrapped
	}
	if id, ok := fields["run_id"].(string); !ok || id == "" {
		return Record[Run]{}, errors.New("avrea returned an invalid workflow-run response")
	}
	return decodeRecord[Run](fields)
}

// SearchLogs retains raw values for display while validating the wire record.
func (c *Client) SearchLogs(ctx context.Context, payload map[string]any) (Record[wire.LogSearchResponse], error) {
	body, err := c.Do(ctx, http.MethodPost, "/logs/search", payload)
	if err != nil {
		return Record[wire.LogSearchResponse]{}, err
	}
	return decodeRecord[wire.LogSearchResponse](body)
}

func (c *Client) Jobs(ctx context.Context, org string, query url.Values) (Page[Job], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs", query)
	if err != nil {
		return Page[Job]{}, err
	}
	return decodePage[Job](body)
}
func (c *Client) Job(ctx context.Context, org, id string, query url.Values) (Record[Job], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs/"+url.PathEscape(id), query)
	if err != nil {
		return Record[Job]{}, err
	}
	if wrapped, ok := body["data"].(map[string]any); ok {
		body = wrapped
	}
	return decodeRecord[Job](body)
}

func (c *Client) SSHForwarding(ctx context.Context, id string) (Record[wire.SSHForwardingResponse], error) {
	body, err := c.Do(ctx, http.MethodPost, "/vms/ssh-forwarding", map[string]any{"job_id": id})
	if err != nil {
		return Record[wire.SSHForwardingResponse]{}, err
	}
	return decodeRecord[wire.SSHForwardingResponse](body)
}

func (c *Client) CancelRun(ctx context.Context, org, id string) (Record[wire.CancelWorkflowRunResponse], error) {
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return Record[wire.CancelWorkflowRunResponse]{}, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord[wire.CancelWorkflowRunResponse](fields)
}

func (c *Client) Rerun(ctx context.Context, org, id string, failed bool) error {
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/workflow-runs/"+url.PathEscape(id)+"/rerun", wire.RerunWorkflowRunRequest{FailedOnly: &failed})
	if err != nil {
		return err
	}
	fields, _ := body["data"].(map[string]any)
	_, err = decodeRecord[wire.RerunWorkflowRunResponse](fields)
	return err
}

func (c *Client) WorkflowAggregates(ctx context.Context, org string, query url.Values) (Page[wire.WorkflowRunAggregateBucket], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/workflow-runs/aggregate", query)
	if err != nil {
		return Page[wire.WorkflowRunAggregateBucket]{}, err
	}
	return decodePage[wire.WorkflowRunAggregateBucket](body)
}
func (c *Client) RepositoryWorkflows(ctx context.Context, org, repo string, query url.Values) (Page[wire.CIWorkflowResponse], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/workflows", query)
	if err != nil {
		return Page[wire.CIWorkflowResponse]{}, err
	}
	return decodePage[wire.CIWorkflowResponse](body)
}

func (c *Client) Repository(ctx context.Context, org, id string) (Record[Repository], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(id), nil)
	if err != nil {
		return Record[Repository]{}, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord[Repository](fields)
}
func (c *Client) DispatchWorkflow(ctx context.Context, org, repo, file, ref string, inputs map[string]string) (Record[wire.DispatchWorkflowResponse], error) {
	request := wire.DispatchWorkflowRequest{Workflow: file, Ref: ref}
	if len(inputs) > 0 {
		request.Inputs.Set(inputs)
	}
	body, err := c.Do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/repos/"+url.PathEscape(repo)+"/dispatch-workflow", request)
	if err != nil {
		return Record[wire.DispatchWorkflowResponse]{}, err
	}
	fields, _ := body["data"].(map[string]any)
	return decodeRecord[wire.DispatchWorkflowResponse](fields)
}

func (c *Client) JobMetrics(ctx context.Context, org, id, source string, query url.Values) (Record[wire.MetricsQueryResponse], error) {
	body, err := c.Get(ctx, "/orgs/"+url.PathEscape(org)+"/jobs/"+url.PathEscape(id)+"/metrics/"+url.PathEscape(source), query)
	if err != nil {
		return Record[wire.MetricsQueryResponse]{}, err
	}
	return decodeRecord[wire.MetricsQueryResponse](body)
}
