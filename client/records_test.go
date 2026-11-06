package client

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRecordPreservesPresenceNumbersAndOpaqueValues(t *testing.T) {
	raw := `{"run_id":"run-fixture","status":"future-state","jobs":[],"conclusion":null,"duration_seconds":0.1234567890123456789,"future_id":900719925474099312345,"future_number":1e1000,"value":{"created_at":false},"summary":"opaque","future":{"run_id":true}}`
	fields, err := decodeObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	record, err := decodeRecord(fields)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	round, err := decodeObject(encoded)
	if err != nil || !reflect.DeepEqual(fields, round) {
		t.Fatalf("response changed: %s (%v)", encoded, err)
	}
	if _, exists := record["completed_at"]; exists {
		t.Fatal("missing field became present")
	}
	if value, exists := record["conclusion"]; !exists || value != nil {
		t.Fatal("explicit null was lost")
	}
	if record["future_id"] != json.Number("900719925474099312345") || record["duration_seconds"] != json.Number("0.1234567890123456789") {
		t.Fatal("number precision changed")
	}
}

func TestRecordRejectsMalformedOperationalFieldsWithoutLeakingValues(t *testing.T) {
	for _, raw := range []string{
		`{"has_more":"true"}`, `{"is_default":"true"}`, `{"enable_remote_desktop":1}`,
		`{"endpoints":{"ssh":{"external_ip":true}}}`,
		`{"endpoints":{"ssh":{"username":true}}}`,
		`{"endpoints":{"remote_desktop":{"external_port":"private-port"}}}`,
		`{"endpoints":{"ssh":{"host_key":false}}}`,
		`{"platform_run_id":"private-id"}`, `{"platform_repository_id":"private-id"}`,
		`{"organization_id":true}`, `{"repository_id":[]}`, `{"state":{}}`,
		`{"created_at":"private-not-a-timestamp"}`, `{"run_attempt":"private-count"}`,
		`{"run_number":1.5}`, `{"port":"private-port"}`, `{"enabled":"false"}`,
		`{"repository":{"repository_id":true}}`, `{"jobs":[{"platform_job_id":"private-id"}]}`,
		`{"steps":["private-step"]}`, `{"duration_seconds":"private-duration"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			fields, err := decodeObject([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeRecord(fields)
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe validation result: %v", err)
			}
		})
	}
}

func TestRecordPreservesLegacyPlatformJobIDSpelling(t *testing.T) {
	for _, id := range []any{json.Number("9007199254740993"), "0009007199254740993"} {
		fields := map[string]any{"jobs": []any{map[string]any{"platform_job_id": id}}}
		record, err := decodeRecord(fields)
		if err != nil || !reflect.DeepEqual(record, fields) {
			t.Fatal(record, err)
		}
	}
}
