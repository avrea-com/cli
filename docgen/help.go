package docgen

import "encoding/json"

type HelpOption struct {
	Names       []string `json:"names"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}
type HelpCommand struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Section     *string `json:"section"`
}
type HelpSpec struct {
	Sections     bool          `json:"sections"`
	Help         string        `json:"help"`
	Epilog       string        `json:"epilog"`
	Usage        []string      `json:"usage"`
	Options      []HelpOption  `json:"options"`
	Commands     []HelpCommand `json:"commands"`
	NoArgsIsHelp bool          `json:"no_args_is_help"`
	SectionOrder []string      `json:"section_order"`
}

// HelpTemplates returns a fresh copy so callers never share mutable help state.
func HelpTemplates() (map[string]HelpSpec, error) {
	var value struct {
		Terminal map[string]HelpSpec `json:"terminal"`
	}
	err := json.Unmarshal(metadata, &value)
	return value.Terminal, err
}
