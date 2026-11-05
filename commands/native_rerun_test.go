package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeRerunReceiptIntegerCoercionAndBounds(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{json.Number("2"), "2"}, {json.Number("2.0"), "2"}, {json.Number("2e0"), "2"},
		{"2", "2"}, {"  +2.00 ", "2"}, {"2_0", "20"}, {"02", "2"},
		{"2e0", ""}, {"2.", ""}, {"2/1", ""}, {"2__0", ""}, {"_2", ""},
		{true, ""}, {nil, ""}, {json.Number("2.5"), ""}, {json.Number("1"), ""},
		{json.Number("1e999999999"), ""}, {json.Number("1e-999999999"), ""}, {strings.Repeat("9", 129), ""},
	} {
		t.Run(stringValueForTest(tc.value), func(t *testing.T) {
			receipt, ok := parseNativeRerunReceipt(map[string]any{"data": map[string]any{"run_id": "awr-" + strings.Repeat("a", 32), "status": "rerun_requested", "run_attempt": tc.value}})
			if tc.want == "" {
				if ok {
					t.Fatalf("accepted invalid receipt: %#v", receipt)
				}
				return
			}
			if !ok || receipt.attempt != tc.want {
				t.Fatalf("receipt %#v, valid %v; want %s", receipt, ok, tc.want)
			}
		})
	}
}

func stringValueForTest(v any) string { raw, _ := json.Marshal(v); return string(raw) }
