package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func ensureDecoderEOF(dec *json.Decoder, label string) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("%s contains trailing JSON value", label)
	}
	return fmt.Errorf("%s trailing data: %w", label, err)
}
