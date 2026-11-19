package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// terminalPrompt reads one line. Hidden prompts go to stderr and disable echo
// when stdin is a terminal.
func terminalPrompt(ctx context.Context, d Dependencies, reader *bufio.Reader, label string, hidden bool) (string, error) {
	writer := d.Out
	if hidden {
		writer = d.Err
	}
	file, isFile := d.In.(*os.File)
	terminal := isFile && d.IsInputTerminal()
	if hidden && !terminal {
		if _, err := fmt.Fprintln(writer, "Warning: Password input may be echoed."); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(writer, label+": "); err != nil {
		return "", err
	}
	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	if hidden && terminal {
		state, err := term.GetState(int(file.Fd()))
		if err != nil {
			return "", err
		}
		defer func() { _ = term.Restore(int(file.Fd()), state) }()
		go func() { raw, err := term.ReadPassword(int(file.Fd())); ch <- result{string(raw), err} }()
	} else {
		go func() {
			value, err := reader.ReadString('\n')
			ch <- result{strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r"), err}
		}()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got := <-ch:
		if got.err != nil && (!errors.Is(got.err, io.EOF) || got.value == "") {
			if hidden {
				_, _ = fmt.Fprintln(d.Out)
			}
			return "", &ExitError{1, "Aborted!"}
		}
		if hidden {
			if _, err := fmt.Fprintln(writer); err != nil {
				return "", err
			}
		}
		return got.value, nil
	}
}
