// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func loginDescription(path string) docgen.Description {
	switch path {
	case "avr login":
		return docgen.Description{
			Help:  "Authenticate via browser and store credentials.",
			Short: "Authenticate via browser and store...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "provider", Kind: "option", Opts: []string{"--provider"}, Type: "text", Nargs: 1, Help: descriptionText("OAuth provider to use for CLI login."), Choices: []string{"google", "github", "gitlab"}}, Label: "--provider [google|github|gitlab]", Suffix: "  [default: github]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "email", Kind: "option", Opts: []string{"--email"}, Type: "text", Nargs: 1, Help: descriptionText("Work email. Routes through your company's SSO if its domain requires it, ignoring --provider.")}, CompletionKind: "String"},
			},
		}
	}
	return docgen.Description{}
}
