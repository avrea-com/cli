package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var source string

func Current() string { return strings.TrimSpace(source) }
