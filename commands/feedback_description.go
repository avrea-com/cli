package commands

import "github.com/avrea-com/cli/docgen"

func feedbackDescription(path string) docgen.Description {
	switch path {
	case "avr feedback":
		return docgen.Description{Usage: []string{"[OPTIONS] MESSAGE | status FEEDBACK_ID"}}
	case "avr feedback status":
		return docgen.Description{Usage: []string{"[OPTIONS]", "FEEDBACK_ID"}}
	}
	return docgen.Description{}
}
