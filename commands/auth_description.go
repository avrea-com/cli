// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func authDescription(path string) docgen.Description {
	switch path {
	case "avr auth device-login":
		return docgen.Description{
			Help: "Sign in from a remote machine using a browser on any device.\n" +
				"\n" +
				"Open the displayed link or scan the QR code, confirm the code, and approve\n" +
				"access. The CLI waits for approval and saves your credentials. No browser\n" +
				"is opened on this machine. QR codes appear only in compatible terminals\n" +
				"wide enough to display them. Requires device-login support on the server.\n",
			Short:          "Sign in from a remote machine using a browser on any device.",
			CompletionHelp: "Sign in from a remote machine using a...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "no_qr", Kind: "option", Opts: []string{"--no-qr"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Hide the terminal QR code; still display the sign-in link.")}, CompletionKind: "Bool"},
			},
		}
	case "avr auth git-credential":
		return docgen.Description{
			Help:  "Git credential protocol adapter, installed by `avr repo clone`.",
			Short: "Git credential protocol adapter, installed...",
			Usage: []string{"[OPTIONS]", "{get|store|erase}"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "url", Kind: "option", Opts: []string{"--url"}, Type: "text", Nargs: 1, Help: descriptionText("Exact clone URL this helper may authenticate.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "full_name", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository whose Git token to request.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "operation", Kind: "argument", Opts: []string{"operation"}, Type: "text", Nargs: 1, Choices: []string{"get", "store", "erase"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr auth login":
		return docgen.Description{
			Help:           "Authenticate via browser and store credentials.",
			Short:          "Authenticate via browser and store credentials.",
			CompletionHelp: "Authenticate via browser and store...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "provider", Kind: "option", Opts: []string{"--provider"}, Type: "choice", Nargs: 1, Help: descriptionText("OAuth provider to use for CLI login."), ShowDefault: true, Default: "github", Choices: []string{"google", "github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "email", Kind: "option", Opts: []string{"--email"}, Type: "text", Nargs: 1, Help: descriptionText("Work email. Routes through your company's SSO if its domain requires it, ignoring --provider.")}, CompletionKind: "String"},
			},
		}
	case "avr auth logout":
		return docgen.Description{
			Help:           "Revoke the current API key and remove stored credentials.",
			Short:          "Revoke the current API key and remove stored credentials.",
			CompletionHelp: "Revoke the current API key and remove...",
		}
	case "avr auth status":
		return docgen.Description{
			Help:           "Display the authenticated user and connection state.",
			Short:          "Display the authenticated user and connection state.",
			CompletionHelp: "Display the authenticated user and...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "show_token", Kind: "option", Opts: []string{"--show-token"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Display the auth token in plain text.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr auth switch":
		return docgen.Description{
			Help: "Switch the default host used when AVR_HOST isn't set.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr auth switch                     # show current default + all hosts\n" +
				"    avr auth switch https://api.avrea.com\n",
			Short:          "Switch the default host used when AVR_HOST isn't set.",
			CompletionHelp: "Switch the default host used when AVR_HOST...",
			Usage:          []string{"[OPTIONS]", "[HOST]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "host", Kind: "argument", Opts: []string{"host"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
			},
		}
	case "avr auth":
		return docgen.Description{
			Help:     "Authenticate and manage credentials.",
			Short:    "Authenticate and manage credentials.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "device-login", Description: "Sign in from a remote machine using a browser on any device.", AutoShort: true},
				{Name: "login", Description: "Authenticate via browser and store credentials.", AutoShort: true},
				{Name: "logout", Description: "Revoke the current API key and remove stored credentials.", AutoShort: true},
				{Name: "status", Description: "Display the authenticated user and connection state.", AutoShort: true},
				{Name: "switch", Description: "Switch the default host used when AVR_HOST isn't set.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
