package spec

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestCommitMetadataRejectsInvalidMessages(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{name: "empty"},
		{name: "contains NUL", message: "before\x00after"},
		{name: "exceeds rune and byte bounds", message: strings.Repeat("😀", MaxCommitMessageRunes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := (CommitMetadata{Message: tc.message}).Validate(); err == nil {
				t.Fatalf("invalid commit message %q was accepted", tc.message)
			}
		})
	}
}

func TestCommitMetadataAcceptsExactSizeBoundsAndMultilineWhitespace(t *testing.T) {
	message := strings.Repeat("😀", MaxCommitMessageRunes)
	if len(message) != MaxCommitMessageBytes {
		t.Fatalf("test message size=%d want configured byte boundary %d", len(message), MaxCommitMessageBytes)
	}
	if err := (CommitMetadata{Message: message}).Validate(); err != nil {
		t.Fatalf("message at exact rune and byte bounds rejected: %v", err)
	}

	message = "first line\nsecond line with trailing spaces  \n"
	contract := strictFeatureContractFixture()
	contract.SchemaVersion = CommitIntentDraftChangeContractSchemaVersion
	contract.Commit = &CommitMetadata{Message: message}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeChangeContract(raw)
	if err != nil {
		t.Fatalf("decode multiline commit intent: %v", err)
	}
	if decoded.Commit == nil {
		t.Fatal("decoded commit intent is absent")
	}
	if decoded.Commit.Message != message {
		t.Fatalf("decoded message=%q want exact %q", decoded.Commit.Message, message)
	}
}

func TestHistoricalChangeContractsRejectCommitMetadata(t *testing.T) {
	for version := LegacyChangeContractSchemaVersion; version <= LockedChangeContractSchemaVersion; version++ {
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			contract := strictFeatureContractFixture()
			contract.SchemaVersion = version
			contract.Commit = &CommitMetadata{Message: "must not change historical schema meaning"}
			raw, err := json.Marshal(contract)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeChangeContract(raw); err == nil {
				t.Fatalf("historical schema v%d accepted commit metadata", version)
			}
		})
	}
}
