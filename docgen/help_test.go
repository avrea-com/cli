package docgen

import (
	"reflect"
	"testing"
)

func TestIndexedHelpMatchesCompleteMetadata(t *testing.T) {
	all, err := HelpTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range all {
		got, known, err := HelpTemplate(path)
		if err != nil || !known || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s indexed help differs from full metadata: known=%v err=%v", path, known, err)
		}
	}
	got, known, err := HelpTemplate("avr custom-extension")
	if err != nil || known || !reflect.DeepEqual(got, HelpSpec{}) {
		t.Fatal("unknown composed command must use its live help", got, known, err)
	}
}

func TestIndexedHelpDoesNotShareMutableState(t *testing.T) {
	want, _, err := HelpTemplate("avr")
	if err != nil {
		t.Fatal(err)
	}
	changed, _, err := HelpTemplate("avr")
	if err != nil {
		t.Fatal(err)
	}
	changed.Usage[0] = "changed"
	changed.Options[0].Names[0] = "--changed"
	changed.CompletionParams[0].Names[0] = "--changed"
	for _, command := range changed.Commands {
		if command.Section != nil {
			*command.Section = "changed"
		}
	}
	got, _, err := HelpTemplate("avr")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("help mutations leaked between callers", err)
	}
}
