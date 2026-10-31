package commands

import (
	"regexp"
	"sync"
)

// Compile command-specific patterns only when used, once per process. Help and
// completion must not pay to initialize unrelated commands. OnceValue also
// preserves MustCompile's panic if a developer introduces an invalid pattern.
func lazyRegexp(pattern string) func() *regexp.Regexp {
	return sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(pattern) })
}
