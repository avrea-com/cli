// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func referenceDescription(path string) docgen.Description {
	if path != "avr reference" {
		return docgen.Description{}
	}
	return docgen.Description{
		Help: "List avr's commands, options and JSON fields.\n" +
			"\n" +
			"Lists every command, or those below the COMMAND words given, from the\n" +
			"same data as the reference documentation. The words may be aliases,\n" +
			"such as groups. With --json each row holds a command's help, its\n" +
			"options and arguments, and the fields its --json accepts, so a script\n" +
			"or coding agent can learn avr without reading help text. It needs no\n" +
			"sign-in and makes no requests.\n" +
			"\n" +
			"Without COMMAND words the first row is avr itself, with its own\n" +
			"options. Each is marked inherited when every command takes it too.\n" +
			"\n" +
			"json_fields lists a command's default fields. It is empty where the\n" +
			"fields depend on the credential or platform, as for auth status and\n" +
			"org install add, and an option such as tests list --summary or secret\n" +
			"get --version-id selects others. The command with --json ? lists the\n" +
			"fields of that invocation.\n" +
			"\n" +
			"\b\n" +
			"Examples:\n" +
			"    avr reference\n" +
			"    avr reference group member\n" +
			"    avr reference --json command,json_fields --jq '.[] | select(.json_fields | length > 0)'\n" +
			"\n" +
			jsonFieldsHelp(referenceFields),
		Short:      "List avr's commands, options and JSON fields.",
		Parameters: serviceTokenJSONParameters(),
	}
}
