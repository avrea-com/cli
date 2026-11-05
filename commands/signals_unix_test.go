//go:build unix

package commands

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestChildRetainsDefaultSIGPIPE(t *testing.T) {
	if os.Getenv("AVR_TEST_SIGPIPE_CHILD") == "1" {
		IgnoreBrokenPipe()
		err := exec.CommandContext(t.Context(), "/bin/sh", "-c", "kill -s PIPE $$; exit 42").Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGPIPE {
			t.Fatalf("child inherited ignored SIGPIPE: %v", err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(t.Context(), executable, "-test.run=^TestChildRetainsDefaultSIGPIPE$")
	child.Env = append(os.Environ(), "AVR_TEST_SIGPIPE_CHILD=1")
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}
