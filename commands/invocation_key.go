package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type invocationKeyValue struct{ target *string }

func (v invocationKeyValue) String() string { return *v.target }
func (v invocationKeyValue) Type() string   { return "uuid" }
func (v invocationKeyValue) Set(value string) error {
	id, err := uuid.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid UUID", pythonQuote(output.CellText(value)))
	}
	*v.target = strings.ReplaceAll(id.String(), "-", "")
	return nil
}

func invocationKeyFlag(c *cobra.Command, key *string, help string) {
	c.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		var invalid *pflag.InvalidValueError
		if errors.As(err, &invalid) && invalid.GetFlag().Name == "idempotency-key" {
			return usageError(cmd, "Invalid value for '--idempotency-key': "+pythonQuote(output.CellText(invalid.GetValue()))+" is not a valid UUID.")
		}
		return err
	})
	c.Flags().Var(invocationKeyValue{key}, "idempotency-key", help)
}

func newInvocationKey() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(id.String(), "-", ""), nil
}
