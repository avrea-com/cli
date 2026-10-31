package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

func TestPagerChild(t *testing.T) {
	mode := os.Getenv("AVR_TEST_PAGER_CHILD")
	if mode == "" {
		return
	}
	if mode == "quit" {
		os.Exit(0)
	}
	if mode == "ignore-interrupt" {
		signal.Ignore(os.Interrupt)
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(2500 * time.Millisecond)
		_, _ = fmt.Fprintln(os.Stdout, "restored")
		os.Exit(0)
	}
	if mode == "wait" {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		select {
		case <-interrupt:
			_, _ = fmt.Fprintln(os.Stdout, "restored")
			os.Exit(0)
		case <-time.After(30 * time.Second):
			os.Exit(3)
		}
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintf(os.Stdout, "paged LESS=%s: %s", os.Getenv("LESS"), data)
	os.Exit(0)
}

func TestPagerConfigurationAndEarlyExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"page", "bypass", "disabled", "quit", "pipe"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AVR_TEST_PAGER_CHILD", "copy")
			t.Setenv("AVR_PAGER", shellquote.Join(executable, "-test.run=^TestPagerChild$"))
			t.Setenv("LESS", "custom")
			d, out, _ := testDeps(t)
			d.LookupEnv = os.LookupEnv
			d.IsTerminal = func() bool { return mode != "pipe" }
			d = d.defaults()
			if mode == "disabled" {
				t.Setenv("AVR_PAGER", "")
			}
			if mode == "quit" {
				t.Setenv("AVR_TEST_PAGER_CHILD", "quit")
			}
			content := "first\nsecond"
			if err := pageOutput(t.Context(), d, content, mode == "bypass"); err != nil {
				t.Fatal(err)
			}
			want := content + "\n"
			if mode == "page" {
				want = "paged LESS=custom: " + want
			}
			if mode == "quit" {
				want = ""
			}
			if out.String() != want {
				t.Fatalf("%q != %q", out.String(), want)
			}
		})
	}
}

type cancelPagerWriter struct {
	cancel context.CancelFunc
	output strings.Builder
}

func (w *cancelPagerWriter) Write(p []byte) (int, error) {
	_, _ = w.output.Write(p)
	w.cancel()
	return len(p), nil
}

func TestPagerCancellationReapsChild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AVR_TEST_PAGER_CHILD", "wait")
	t.Setenv("AVR_PAGER", shellquote.Join(executable, "-test.run=^TestPagerChild$"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d, _, _ := testDeps(t)
	writer := &cancelPagerWriter{cancel: cancel}
	d.Out = writer
	d.IsTerminal = func() bool { return true }
	d.LookupEnv = os.LookupEnv
	d = d.defaults()
	if err := pageOutput(ctx, d, "content", false); !errors.Is(err, errPagerInterrupted) {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && !strings.Contains(writer.output.String(), "restored") {
		t.Fatal("pager did not get a chance to restore the terminal")
	}
	root := &cobra.Command{Use: "pager", RunE: run(func(*cobra.Command, []string) error { return errPagerInterrupted })}
	if code := ExecuteRoot(ctx, root, nil, d.Err); code != 0 {
		t.Fatal("pager interruption must exit successfully", code)
	}
}

func TestLogColorsAreReadableAndControlsCannotExecute(t *testing.T) {
	var b strings.Builder
	writeStyledLogLine(&b, map[string]any{"content": "\x1b[31mfailed\x1b[0m\nnext\x1b]2;unexpected\a"}, Dependencies{IsTerminal: func() bool { return false }}.defaults(), "")
	value := b.String()
	if !strings.HasPrefix(value, "failed\nnext") {
		t.Fatalf("colors obscure readable log content: %s", strconv.Quote(value))
	}
	if strings.Contains(value, "\x1b") {
		t.Fatalf("external control sequence escaped sanitization: %s", strconv.Quote(value))
	}
}

func TestStyledLogsKeepColorsButEscapeTerminalCommands(t *testing.T) {
	var b strings.Builder
	d := Dependencies{IsTerminal: func() bool { return true }, Getenv: func(string) string { return "" }}.defaults()
	writeStyledLogLine(&b, map[string]any{"content": "\x1b[31mfailed\x1b[0m\x1b]2;unexpected\a"}, d, "")
	if !strings.Contains(b.String(), "\x1b[31mfailed\x1b[0m") || strings.Contains(b.String(), "\x1b]2;") {
		t.Fatalf("log color/control boundary: %q", b.String())
	}
}

func TestLogTabsPreserveIndentationAndLiteralEscapes(t *testing.T) {
	var b strings.Builder
	d := Dependencies{IsTerminal: func() bool { return false }}.defaults()
	writeStyledLogLine(&b, map[string]any{"content": "actual\ttab; literal\\ttext"}, d, "")
	if b.String() != "actual\ttab; literal\\ttext\n" {
		t.Fatalf("log whitespace changed: %q", b.String())
	}
}

type interruptPagerWriter struct {
	output strings.Builder
	sent   bool
}

func (w *interruptPagerWriter) Write(p []byte) (int, error) {
	_, _ = w.output.Write(p)
	if !w.sent {
		w.sent = true
		process, _ := os.FindProcess(os.Getpid())
		_ = process.Signal(os.Interrupt)
	}
	return len(p), nil
}
func TestPagerOwnsTerminalInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal interrupt delivery")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AVR_TEST_PAGER_CHILD", "ignore-interrupt")
	t.Setenv("AVR_PAGER", shellquote.Join(executable, "-test.run=^TestPagerChild$"))
	t.Setenv("LESS", "-R")
	ctx, stop := InterruptContext(t.Context())
	defer stop()
	d, _, _ := testDeps(t)
	writer := &interruptPagerWriter{}
	d.Out = writer
	d.IsTerminal = func() bool { return true }
	d.LookupEnv = os.LookupEnv
	d = d.defaults()
	if err := pageOutput(ctx, d, "content", false); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || !strings.Contains(writer.output.String(), "restored") {
		t.Fatal("pager was killed", ctx.Err(), writer.output.String())
	}
	policy, _ := ctx.Value(interruptPolicyKey{}).(*interruptPolicy)
	if policy.childOwnsInterrupt.Load() {
		t.Fatal("pager retained interrupt ownership")
	}
}
