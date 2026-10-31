package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
)

type cpuProfileKey struct{}

// Execute includes command construction; ExecuteRoot covers embedded consumers.
// The context marker prevents starting a second profiler at the nested boundary.
// Runtime/package initialization precedes both: use GODEBUG=inittrace=1 for that.
func startCPUProfile(ctx context.Context, d Dependencies) (context.Context, func() error, error) {
	if ctx.Value(cpuProfileKey{}) != nil || d.Getenv("_AVR_COMPLETE") != "" {
		return ctx, nil, nil
	}
	path := d.Getenv("AVR_CPU_PROFILE")
	if path == "" {
		return ctx, nil, nil
	}
	// A diagnostic run must never truncate an existing file or follow a symlink.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ctx, nil, fmt.Errorf("create CPU profile: %w", err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return ctx, nil, fmt.Errorf("start CPU profile: %w", err)
	}
	return context.WithValue(ctx, cpuProfileKey{}, true), func() error {
		pprof.StopCPUProfile()
		return file.Close()
	}, nil
}

func profileFailure(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "Error: CPU profiling: %v\n", err)
	return 1
}

func finishProfile(finish func() error, stderr io.Writer, code *int) {
	if err := finish(); err != nil {
		_ = profileFailure(stderr, err)
		if *code == 0 {
			*code = 1
		}
	}
}
