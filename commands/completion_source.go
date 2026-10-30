package commands

import (
	_ "embed"
	"strings"
	"unicode"

	"github.com/kballard/go-shellquote"
)

// These scripts consume the compatibility protocol; they do not invoke Cobra's
// unrelated __complete protocol or depend on a Python installation.
//
//go:embed completion/bash.sh
var completionBash string

//go:embed completion/zsh.sh
var completionZsh string

//go:embed completion/fish.fish
var completionFish string

func completionSource(shell, program, variable string) string {
	source := completionBash
	switch shell {
	case "zsh":
		source = completionZsh
	case "fish":
		source = completionFish
	}
	identifier := "_" + strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '_'
	}, program) + "_go_complete"
	return strings.NewReplacer("__FUNCTION__", identifier, "__VARIABLE__", variable, "__PROGRAM__", shellquote.Join(program)).Replace(source)
}
