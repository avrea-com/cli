package wire

import (
	"encoding/json"
	"testing"
)

func TestWorkflowRunNullablePresenceAndPrecision(t *testing.T) {
	for _, tc := range []struct {
		name, payload   string
		specified, null bool
	}{
		{"absent", `{"run_id":"run-019a0000000070008000000000000001","status":"queued","duration_seconds":0.123456789012345}`, false, false},
		{"null", `{"jobs":null}`, true, true},
		{"empty", `{"jobs":[]}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var run WorkflowRunResponse
			if err := json.Unmarshal([]byte(tc.payload), &run); err != nil {
				t.Fatal(err)
			}
			if run.Jobs.IsSpecified() != tc.specified || run.Jobs.IsNull() != tc.null {
				t.Fatalf("wrong jobs presence: %#v", run.Jobs)
			}
			encoded, err := json.Marshal(run)
			if err != nil {
				t.Fatal(err)
			}
			var round map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &round); err != nil {
				t.Fatal(err)
			}
			_, present := round["jobs"]
			if present != tc.specified {
				t.Fatalf("jobs presence changed: %s", encoded)
			}
			if tc.null && string(round["jobs"]) != "null" {
				t.Fatalf("null lost: %s", encoded)
			}
			if tc.name == "absent" && run.DurationSeconds.GetOrEmpty() != 0.123456789012345 {
				t.Fatalf("float precision lost: %v", run.DurationSeconds)
			}
		})
	}
}

func TestRepositoryAdditionalPropertiesAndEnum(t *testing.T) {
	var repository WorkflowRunRepository
	if err := json.Unmarshal([]byte(`{"id":"rep-019a0000000070008000000000000001","full_name":"acme/web","mirror_enabled":null,"future":{"enabled":true}}`), &repository); err != nil {
		t.Fatal(err)
	}
	if !repository.MirrorEnabled.IsNull() {
		t.Fatal("nullable bool lost")
	}
	encoded, err := json.Marshal(repository)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	future, ok := result["future"].(map[string]any)
	if !ok || future["enabled"] != true {
		t.Fatalf("additional properties lost: %s", encoded)
	}
	if !WorkflowRunResponseStatus("in_progress").Valid() || WorkflowRunResponseStatus("bogus").Valid() {
		t.Fatal("status enum contract differs")
	}
}
