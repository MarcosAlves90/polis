package spec

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeCommitIntentRejectsInvalidUTF8(t *testing.T) {
	contract := strictFeatureContractFixture()
	contract.SchemaVersion = CommitIntentDraftChangeContractSchemaVersion
	contract.Commit = &CommitMetadata{Message: "valid"}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte(`"message":"valid"`)
	invalid := append([]byte(`"message":"`), 0xff)
	invalid = append(invalid, '"')
	if !bytes.Contains(raw, valid) {
		t.Fatal("test contract does not contain the expected message encoding")
	}
	raw = bytes.Replace(raw, valid, invalid, 1)
	if _, err := DecodeChangeContract(raw); err == nil {
		t.Fatal("schema-v5 contract with invalid UTF-8 message was accepted")
	}
}
