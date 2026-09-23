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

func TestDecodeChangeContractRejectsUnpairedCommitMessageSurrogates(t *testing.T) {
	cases := []struct {
		name        string
		rawJSONText string
	}{
		{name: "isolated high surrogate", rawJSONText: `"feat: invalid \ud800"`},
		{name: "isolated low surrogate", rawJSONText: `"feat: invalid \udc00"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if contract, err := DecodeChangeContract(commitContractJSONWithRawMessage(t, tc.rawJSONText)); err == nil {
				got := "<missing>"
				if contract.Commit != nil {
					got = contract.Commit.Message
				}
				t.Fatalf("accepted unpaired surrogate as commit.message %q", got)
			}
		})
	}
}

func TestDecodeChangeContractPreservesValidCommitMessageSurrogatePair(t *testing.T) {
	const expected = "feat: emoji 😀"
	contract, err := DecodeChangeContract(commitContractJSONWithRawMessage(t, `"feat: emoji \ud83d\ude00"`))
	if err != nil {
		t.Fatalf("decode valid surrogate pair: %v", err)
	}
	if contract.Commit == nil || contract.Commit.Message != expected {
		t.Fatalf("decoded commit message=%q want exact %q", contract.Commit.Message, expected)
	}

	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := DecodeChangeContract(raw)
	if err != nil {
		t.Fatalf("decode round-tripped contract: %v", err)
	}
	if roundTrip.Commit == nil || roundTrip.Commit.Message != expected {
		t.Fatalf("round-tripped commit message=%q want exact %q", roundTrip.Commit.Message, expected)
	}
}

func TestDecodeChangeContractRejectsUnknownCommitMetadataFields(t *testing.T) {
	raw := commitContractJSONWithRawMessage(t, `"feat: valid"`)
	const message = `"message":"feat: valid"`
	if !strings.Contains(string(raw), message) {
		t.Fatalf("encoded contract is missing expected commit message: %s", raw)
	}
	raw = []byte(strings.Replace(string(raw), message, message+`,"unexpected":true`, 1))
	if _, err := DecodeChangeContract(raw); err == nil {
		t.Fatal("accepted unknown commit metadata field")
	}
}

func commitContractJSONWithRawMessage(t *testing.T, rawJSONText string) []byte {
	t.Helper()
	contract := strictFeatureContractFixture()
	contract.SchemaVersion = CommitIntentDraftChangeContractSchemaVersion
	contract.Commit = &CommitMetadata{Message: "POLIS_COMMIT_MESSAGE_MARKER"}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	const marker = `"message":"POLIS_COMMIT_MESSAGE_MARKER"`
	if strings.Count(string(raw), marker) != 1 {
		t.Fatalf("expected one commit message marker in encoded contract: %s", raw)
	}
	return []byte(strings.Replace(string(raw), marker, `"message":`+rawJSONText, 1))
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
