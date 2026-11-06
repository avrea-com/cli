// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func logoutDescription(path string) docgen.Description {
	switch path {
	case "avr logout":
		return docgen.Description{
			Help:  "Revoke the current API key and remove stored credentials.",
			Short: "Revoke the current API key and remove...",
		}
	}
	return docgen.Description{}
}
