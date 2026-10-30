package commands

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newInvoiceDownload(d Dependencies) *cobra.Command {
	var org, target string
	c := &cobra.Command{Use: "download INVOICE_ID", Short: "Download an invoice PDF.", Args: positionalArguments("INVOICE_ID")}
	orgFlag(c, &org)
	c.Flags().StringVar(&target, "out", "", "Output file path. Defaults to <invoice_id>.pdf.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		stream, err := api.DownloadInvoice(cmd.Context(), billingBase(id)+"/invoices/"+url.PathEscape(args[0])+"/pdf")
		downloadError := func(err error) error {
			var failure *client.DownloadError
			if errors.As(err, &failure) {
				return errors.New("invoice PDF download failed: connection or stream interrupted")
			}
			return apiError(cfg, err, "download invoice PDF")
		}
		if err != nil {
			return downloadError(err)
		}
		spool, _, err := spoolDownload(stream)
		if err != nil {
			return downloadError(err)
		}
		defer func() { _ = spool.Close(); _ = os.Remove(spool.Name()) }()
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		filename := target
		if filename == "" {
			// Match os.path.basename, including its empty result for a trailing separator.
			safe := filepath.Base(args[0])
			if args[0] == "" || strings.HasSuffix(args[0], string(filepath.Separator)) {
				safe = ""
			}
			filename = safe + ".pdf"
		}
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
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
		_, err = fmt.Fprintln(d.Out, "Invoice PDF saved to "+output.CellText(filename))
		return err
	})
	return c
}
