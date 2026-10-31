package commands

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var sbomArtifactNames = map[string]string{"cyclonedx": "bom.cyclonedx.json", "spdx": "bom.spdx.json", "inventory": "inventory.json"}

func newSBOMDownload(d Dependencies) *cobra.Command {
	var org, repo, commit, format, target string
	c := &cobra.Command{Use: "download [COMMIT]", Short: "Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).", Args: cobra.MaximumNArgs(1)}
	cacheScopeFlags(c, &org, &repo)
	f := c.Flags()
	f.StringVar(&commit, "commit", "", "Full SHA or unique prefix.")
	f.StringVar(&format, "format", "cyclonedx", "Artifact format: cyclonedx, inventory, or spdx.")
	f.StringVar(&target, "out", "", "Output filename, or - for stdout; defaults to the artifact filename.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		filename, ok := sbomArtifactNames[format]
		if !ok {
			return &ExitError{2, "Error: invalid --format: " + output.CellText(format)}
		}
		sha, err := requestedSBOMCommit(args, commit, f.Changed("commit"))
		if err != nil {
			return err
		}
		cfg, api, id, rid, err := sbomContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		base := sbomBase(id, rid)
		if sha == "" {
			record, e := api.SBOM(cmd.Context(), base+"/latest")
			if e != nil {
				return sbomError(cfg, e, "get SBOM", true)
			}
			sha = fieldText(record.Fields, "commit_sha")
		} else {
			sha, err = resolveSBOMCommit(cmd.Context(), cfg, api, base, sha)
			if err != nil {
				return err
			}
		}
		stream, err := api.DownloadDecoded(cmd.Context(), base+"/commits/"+url.PathEscape(sha)+"/artifacts/"+filename)
		if err != nil {
			return sbomDownloadError(cfg, err, filename)
		}
		spool, count, err := spoolDownload(stream)
		if err != nil {
			return sbomDownloadError(cfg, err, filename)
		}
		defer func() { _ = spool.Close(); _ = os.Remove(spool.Name()) }()
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if target == "-" {
			_, err = io.Copy(d.Out, spool)
			return err
		}
		destination := target
		if destination == "" {
			destination = filename
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err == nil {
			_, err = io.Copy(file, spool)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			_, _ = fmt.Fprintf(d.Err, "Error writing file: %s\n", output.CellText(err.Error()))
			return &ExitError{1, "Aborted!"}
		}
		short := []rune(sha)
		if len(short) > 12 {
			short = short[:12]
		}
		_, err = fmt.Fprintf(d.Out, "Saved %s for %s to %s (%s).\n", filename, output.CellText(string(short)), output.CellText(destination), formatSize(count))
		return err
	})
	return c
}

func sbomDownloadError(cfg config.Config, err error, filename string) error {
	var stream *client.DownloadError
	if errors.As(err, &stream) {
		return errors.New("SBOM download failed: connection or stream interrupted")
	} //nolint:staticcheck // User-facing command diagnostic.
	return sbomError(cfg, err, "download "+filename, true)
}
