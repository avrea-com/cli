package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type GitLabRunnerError struct{ Message string }

func (e *GitLabRunnerError) Error() string { return e.Message }

// CreateGitLabRunner sends the personal token only to GitLab's fixed origin.
// It does not inherit API authentication, tracing, cookies, or redirects.
func (c *Client) CreateGitLabRunner(ctx context.Context, token string, body map[string]any) (string, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://gitlab.com/api/v4/user/runners", bytes.NewReader(raw))
	if err != nil {
		return "", "", errors.New("could not construct GitLab request")
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")
	h := *c.HTTP
	h.Timeout = 30 * time.Second
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := h.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", &GitLabRunnerError{"Could not reach GitLab. Check the group's runners before retrying creation."}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 201 {
		return "", "", &GitLabRunnerError{fmt.Sprintf("GitLab runner creation failed (HTTP %d). Check Owner role and create_runner scope.", resp.StatusCode)}
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	invalid := &GitLabRunnerError{"GitLab returned invalid runner metadata. Check the group's runners before retrying."}
	if err != nil || len(raw) > 1<<20 {
		return "", "", invalid
	}
	result, err := decodeObject(raw)
	if err != nil {
		return "", "", invalid
	}
	id, ok := result["id"].(json.Number)
	if !ok {
		return "", "", invalid
	}
	n, err := id.Int64()
	runnerToken, ok := result["token"].(string)
	if err != nil || n <= 0 || !ok || runnerToken == "" {
		return "", "", invalid
	}
	return id.String(), runnerToken, nil
}
