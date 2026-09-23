package packageverify

import (
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestChangeContractFormatCompatibility(t *testing.T) {
	tests := []struct {
		name, wantError string
		format, schema  int
	}{
		{name: "historical locked v4 in package v4", format: 4, schema: spec.LockedChangeContractSchemaVersion},
		{name: "historical schemas preserved in package v4", format: 4, schema: spec.StrictChangeContractSchemaVersion},
		{name: "locked commit intent in package v5", format: spec.FormatVersion, schema: spec.CommitIntentLockedChangeContractSchemaVersion},
		{name: "draft commit intent is never package content", format: spec.FormatVersion, schema: spec.CommitIntentDraftChangeContractSchemaVersion, wantError: "draft Change Contract schema v5"},
		{name: "locked v6 is not allowed in historical package", format: 4, schema: spec.CommitIntentLockedChangeContractSchemaVersion, wantError: "requires package format v5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateChangeContractFormatCompatibility(tt.format, tt.schema)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v, want substring %q", err, tt.wantError)
			}
		})
	}
}
