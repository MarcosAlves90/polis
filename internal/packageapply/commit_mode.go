package packageapply

import (
	"errors"
	"fmt"
)

var ErrCommitBlocked = errors.New("artifact-backed commit blocked")

type CommitMode string

const (
	CommitModeNone   CommitMode = "none"
	CommitModePrompt CommitMode = "prompt"
	CommitModeAuto   CommitMode = "auto"
)

type CommitConfirmation func(message, targetTree string) (bool, error)

func ParseCommitMode(value string) (CommitMode, error) {
	if value == "" {
		return CommitModeNone, nil
	}
	mode := CommitMode(value)
	switch mode {
	case CommitModeNone, CommitModePrompt, CommitModeAuto:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid commit mode %q: want none, prompt, or auto", value)
	}
}
