package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var artifactFields = []string{"id", "name", "repository_id", "workflow_run_id", "run_kind", "run_attempt", "size_bytes", "digest_sha256", "file_name", "file_count", "archived", "state", "created_at", "completed_at", "expires_at"}
var artifactDownloadFields = []string{"id", "path", "size_bytes", "digest_sha256"}
var artifactDeleteFields = []string{"id", "deleted"}
var artifactIDPattern = lazyRegexp("^art-[0-9a-f]{32}$")
var artifactDigestPattern = lazyRegexp("^[0-9a-f]{64}$")

// NewArtifact shares artifact inspection and verified file transfers with admin.
func NewArtifact(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "artifact", Short: "Manage workflow artifacts."}
	group.AddCommand(newArtifactList(d))
	for _, action := range []string{"get", "download", "delete"} {
		group.AddCommand(newArtifactRecord(d, action))
	}
	return group
}

func artifactScope(ctx context.Context, d Dependencies, org, repo string) (config.Config, *client.Client, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	if err != nil {
		return cfg, api, "", err
	}
	repository, err := requiredWorkflowRepo(ctx, d, cfg, api, id, repo)
	if err != nil {
		return cfg, api, "", err
	}
	return cfg, api, "/orgs/" + url.PathEscape(id) + "/repos/" + url.PathEscape(repository) + "/artifacts", nil
}

func artifactError(cfg config.Config, err error) error {
	var transport *client.TransportError
	if errors.As(err, &transport) {
		return &client.DownloadError{}
	}
	return apiError(cfg, err, "access workflow artifact")
}

func newArtifactList(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, runID, cursor string
	var limit int
	c := &cobra.Command{Use: "list REPO", Short: "List artifacts in REPO, optionally restricted to one run.", Args: cobra.ExactArgs(1)}
	o.flags(c)
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&runID, "run-id", "", "Numeric GitHub run ID, run- ID, or native awr- ID.")
	f.IntVar(&limit, "limit", 50, "Maximum artifacts per page (1–1000).")
	f.StringVar(&cursor, "cursor", "", "Continue from a previous page's cursor.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, artifactFields); done || err != nil {
			return err
		}
		cfg, api, base, err := artifactScope(cmd.Context(), d, org, args[0])
		if err != nil {
			return err
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if runID != "" {
			query.Set("run_id", "eq."+runID)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.Artifacts(cmd.Context(), base, query)
		if err != nil {
			return artifactError(cfg, err)
		}
		rows := rawRecords(page)
		if o.fields != "" {
			err = o.list(cmd, d, rows, artifactFields)
		} else {
			err = renderArtifactList(d, rows)
		}
		if err != nil {
			return err
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "More artifacts available. Continue with --cursor %s\n", output.CellText(page.NextCursor))
		}
		return err
	})
	return c
}

func newArtifactRecord(d Dependencies, action string) *cobra.Command {
	var o jsonOptions
	var org, repo, destination string
	var yes bool
	fields := artifactFields
	if action == "download" {
		fields = artifactDownloadFields
	}
	if action == "delete" {
		fields = artifactDeleteFields
	}
	c := &cobra.Command{Use: action + " ART_ID", Short: strings.ToUpper(action[:1]) + action[1:] + " a workflow artifact.", Args: cobra.ExactArgs(1)}
	o.flags(c)
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug; defaults to the configured organization.")
	f.StringVar(&repo, "repo", "", "Repository name or ID; defaults to the current git repository.")
	if action == "download" {
		f.StringVar(&destination, "path", ".", "Destination directory or filename. ZIPs stay archived.")
	}
	if action == "delete" {
		f.BoolVarP(&yes, "yes", "y", false, "Skip confirmation.")
	}
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, fields); done || err != nil {
			return err
		}
		cfg, api, base, err := artifactScope(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		id := args[0]
		if !artifactIDPattern().MatchString(id) {
			return &ExitError{2, "Error: Expected an Avrea art- ID"}
		}
		endpoint := base + "/" + id
		var row map[string]any
		var raw []byte
		if action == "delete" {
			if !yes {
				if err = confirmMutation(cmd.Context(), d, "deleting an artifact", "Delete "+id+"?"); err != nil {
					return err
				}
			}
			if _, err = api.Do(cmd.Context(), http.MethodDelete, endpoint, nil); err != nil {
				return artifactError(cfg, err)
			}
			row = map[string]any{"id": id, "deleted": true}
			raw = []byte(fmt.Sprintf("{\"id\":%q,\"deleted\":true}", id))
		} else {
			record, e := api.Artifact(cmd.Context(), endpoint)
			if e != nil {
				return artifactError(cfg, e)
			}
			row, raw = record.Fields, record.RawJSON
			if action == "download" {
				path, e := downloadArtifact(cmd.Context(), api, endpoint+"/download", row, destination)
				if e != nil {
					return artifactError(cfg, e)
				}
				row = map[string]any{"id": id, "path": path, "size_bytes": row["size_bytes"], "digest_sha256": row["digest_sha256"]}
				// Struct declaration order is the public Python record order.
				raw, err = json.Marshal(struct {
					ID     string `json:"id"`
					Path   string `json:"path"`
					Size   any    `json:"size_bytes"`
					Digest any    `json:"digest_sha256"`
				}{id, path, row["size_bytes"], row["digest_sha256"]})
				if err != nil {
					return err
				}
			}
		}
		if o.fields != "" {
			selected, e := output.Fields(o.fields, fields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, row, selected, o.expression)
		}
		return renderOrderedRecord(d.Out, raw)
	})
	return c
}

// Ordered JSON decoding preserves Python str() values and API member order.
func renderOrderedRecord(out io.Writer, raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	record, err := readWorkflowInput(decoder, 0)
	if err != nil {
		return err
	}
	if len(record.order) == 0 {
		_, err = fmt.Fprintln(out, "No data.")
		return err
	}
	width := 0
	for _, key := range record.order {
		width = max(width, utf8.RuneCountInString(output.CellText(key)))
	}
	for _, key := range record.order {
		if _, err = fmt.Fprintf(out, "%s%s%s\n", output.CellText(key), strings.Repeat(" ", width-utf8.RuneCountInString(output.CellText(key))+2), output.CellText(record.fields[key])); err != nil {
			return err
		}
	}
	return nil
}

func pythonValue(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	result, err := readWorkflowInput(decoder, 0)
	if err != nil {
		return fmt.Sprint(value)
	}
	return result.text
}
func renderArtifactList(d Dependencies, rows []map[string]any) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	headers := []string{"id", "name", "workflow_run_id", "size_bytes", "state", "expires_at"}
	var cells [][]string
	for _, row := range rows {
		values := make([]string, len(headers))
		for i, key := range headers {
			value, ok := row[key]
			if !ok {
				values[i] = "N/A"
			} else {
				values[i] = pythonValue(value)
			}
		}
		cells = append(cells, values)
	}
	return output.Grid(d.Out, headers, cells)
}

func expandArtifactPath(path string) (string, error) {
	if path == "" {
		return ".", nil
	}
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, "~\\") {
		if !strings.HasPrefix(path, "~") {
			return path, nil
		}
		name, rest, _ := strings.Cut(path[1:], "/")
		account, err := user.Lookup(name)
		if err != nil {
			return "", errors.New("Could not determine home directory.") //nolint:staticcheck // Preserve the Python user-facing diagnostic.
		}
		return filepath.Join(account.HomeDir, rest), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
func downloadArtifact(ctx context.Context, api *client.Client, endpoint string, row map[string]any, destination string) (string, error) {
	size, ok := row["size_bytes"].(json.Number)
	count, err := size.Int64()
	digest, digestOK := row["digest_sha256"].(string)
	if row["state"] != "complete" || !ok || err != nil || count < 0 || count > 10<<30 || !digestOK || !artifactDigestPattern().MatchString(digest) {
		return "", errors.New("Artifact is unavailable or has invalid integrity metadata") //nolint:staticcheck // Preserve the Python user-facing diagnostic.
	}
	name, ok := row["file_name"].(string)
	if !ok || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return "", errors.New("Artifact has an invalid filename") //nolint:staticcheck // Preserve the Python user-facing diagnostic.
	}
	destination, err = expandArtifactPath(destination)
	if err != nil {
		return "", err
	}
	if info, e := os.Stat(destination); e == nil && info.IsDir() {
		destination = filepath.Join(destination, name)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".avr-artifact-")
	if err != nil {
		return "", err
	}
	defer func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }()
	stream, err := api.Download(ctx, endpoint)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	hash := sha256.New()
	received, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(stream, count+1))
	if err != nil {
		return "", err
	}
	if received > count {
		return "", errors.New("Artifact exceeds its recorded size") //nolint:staticcheck // Preserve the Python user-facing diagnostic.
	}
	if received != count || hex.EncodeToString(hash.Sum(nil)) != digest {
		return "", errors.New("Artifact size or SHA-256 verification failed") //nolint:staticcheck // Preserve the Python user-facing diagnostic.
	}
	if err = temporary.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(temporary.Name(), destination); err != nil {
		return "", err
	}
	return destination, nil
}
