package docgen

import (
	"github.com/spf13/cobra"
	"slices"
	"strings"
)

type HelpOption struct {
	Names       []string `json:"names"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}
type HelpCommand struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Section     *string `json:"section"`
	AutoShort   bool    `json:"auto_short,omitempty"`
}
type CompletionType struct {
	Kind          string   `json:"param_type"`
	Choices       []string `json:"choices"`
	CaseSensitive bool     `json:"case_sensitive"`
	FileOkay      bool     `json:"file_okay"`
}
type CompletionParam struct {
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Names    []string       `json:"names"`
	Help     string         `json:"help"`
	Multiple bool           `json:"multiple"`
	Flag     bool           `json:"flag"`
	Hidden   bool           `json:"hidden"`
	Nargs    int            `json:"nargs"`
	Type     CompletionType `json:"type"`
}
type HelpSpec struct {
	CompletionHelp   string            `json:"completion_help"`
	CompletionParams []CompletionParam `json:"completion_params"`
	Sections         bool              `json:"sections"`
	Help             string            `json:"help"`
	Epilog           string            `json:"epilog"`
	Usage            []string          `json:"usage"`
	Options          []HelpOption      `json:"options"`
	Commands         []HelpCommand     `json:"commands"`
	NoArgsIsHelp     bool              `json:"no_args_is_help"`
	SectionOrder     []string          `json:"section_order"`
}

// Parameter is shared by reference docs, terminal help and completion.
// Param holds semantic details absent from Cobra's string-valued flags.
type Parameter struct {
	Param
	Label, Suffix, DisplayHelp string
	CompletionKind             string
	CaseSensitive, FileOkay    bool
}

func (p Parameter) label() string {
	if p.Label != "" {
		return p.Label
	}
	names := append([]string{}, p.Opts...)
	slices.SortStableFunc(names, func(a, b string) int {
		if strings.HasPrefix(a, "--") == strings.HasPrefix(b, "--") {
			return 0
		}
		if strings.HasPrefix(a, "--") {
			return 1
		}
		return -1
	})
	label := strings.Join(names, ", ")
	if len(p.Secondary) > 0 {
		label += " / " + strings.Join(p.Secondary, ", ")
	}
	if !p.IsFlag {
		typ := strings.ToUpper(p.Type)
		if p.Type == "choice" {
			typ = "[" + strings.Join(p.Choices, "|") + "]"
		}
		label += " " + typ
	}
	return label
}

type Description struct {
	Help, Short, CompletionHelp, Epilog string
	Usage                               []string
	Sections                            bool
	SectionOrder                        []string
	Parameters                          []Parameter
	Commands                            []HelpCommand
}
type Describer func(*cobra.Command) Description

type Section struct {
	Name     string
	Commands []string
}

func (d Description) params() []Param {
	out := make([]Param, len(d.Parameters))
	for i, p := range d.Parameters {
		out[i] = p.Param
		if out[i].Secondary == nil {
			out[i].Secondary = []string{}
		}
	}
	return out
}

// Help derives all three terminal views from one Go-owned parameter description.
// Callers still filter against live flags, so composed removals/additions work.
func Help(c *cobra.Command, describe Describer) HelpSpec {
	d := describe(c)
	spec := HelpSpec{Help: d.Help, Epilog: d.Epilog, Usage: d.Usage, Sections: d.Sections,
		SectionOrder: d.SectionOrder, Commands: d.Commands, CompletionHelp: d.CompletionHelp}
	if spec.CompletionHelp == "" {
		spec.CompletionHelp = d.Short
	}
	if spec.CompletionHelp == "" {
		spec.CompletionHelp = c.Short
	}
	for _, p := range d.Parameters {
		names := append(append([]string{}, p.Opts...), p.Secondary...)
		spec.CompletionParams = append(spec.CompletionParams, CompletionParam{
			Name: p.Name, Kind: p.Kind, Names: names, Help: text(p.Help), Multiple: p.Multiple,
			Flag: p.IsFlag, Hidden: p.Hidden, Nargs: p.Nargs, Type: CompletionType{
				Kind: p.CompletionKind, Choices: p.Choices, CaseSensitive: p.CaseSensitive, FileOkay: p.FileOkay}})
		if p.Kind == "option" && !p.Hidden {
			description := text(p.Help) + p.Suffix
			if p.DisplayHelp != "" {
				description = p.DisplayHelp
			}
			// The declared default remains authoritative after composition.
			for _, name := range names {
				if flag := lookupFlag(c, name); flag != nil && p.ShowDefault && p.Default != nil {
					description = strings.Replace(description, "default: "+pythonValue(p.Default), "default: "+pythonValue(flagDefault(flag)), 1)
					break
				}
			}
			spec.Options = append(spec.Options, HelpOption{Names: names, Label: p.label(), Description: description})
		}
	}
	if d.Help != "" {
		label := "-h, --help"
		spec.Options = append(spec.Options, HelpOption{Names: []string{"-h", "--help"}, Label: label, Description: "Show this message and exit."})
		spec.CompletionParams = append(spec.CompletionParams, CompletionParam{Name: "_click_default_help", Kind: "option", Names: []string{"-h", "--help"}, Help: "Show this message and exit.", Flag: true, Nargs: 1, Type: CompletionType{Kind: "Bool"}})
	}
	return spec
}
