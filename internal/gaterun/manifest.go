package gaterun

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const MaxManifestBytes = 1 << 20

type Identity struct {
	SHA256     string            `json:"sha256"`
	Categories map[string]string `json:"categories"`
}

type Observation struct {
	ExitCode     int    `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	StdoutSHA256 string `json:"stdout_sha256"`
	StderrSHA256 string `json:"stderr_sha256"`
}

type Gate struct {
	ID              string          `json:"id"`
	Definition      spec.GatePolicy `json:"definition"`
	Identity        Identity        `json:"identity"`
	Status          spec.Status     `json:"status"`
	Action          string          `json:"action"`
	Reason          string          `json:"reason,omitempty"`
	StaleCategories []string        `json:"stale_categories,omitempty"`
	ReusedFrom      string          `json:"reused_from,omitempty"`
	Observation     *Observation    `json:"observation,omitempty"`
}

type Manifest struct {
	SchemaVersion   int         `json:"schema_version"`
	RunID           string      `json:"run_id"`
	Nonce           string      `json:"nonce"`
	ReplayOf        string      `json:"replay_of,omitempty"`
	Inputs          Inputs      `json:"inputs"`
	SelectedGates   []string    `json:"selected_gates"`
	SelectionReason string      `json:"selection_reason"`
	ChangedPaths    []string    `json:"changed_paths"`
	Jobs            int         `json:"jobs"`
	Current         bool        `json:"current"`
	Status          spec.Status `json:"status"`
	Gates           []Gate      `json:"gates"`
}

func (m *Manifest) seal() error {
	if m.Nonce == "" {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		m.Nonce = hex.EncodeToString(nonce[:])
	}
	m.RunID = ""
	m.RunID = digest(*m)
	return nil
}

func Load(filename string) (Manifest, error) {
	initial, err := os.Stat(filename)
	if err != nil {
		return Manifest{}, err
	}
	if !initial.Mode().IsRegular() {
		return Manifest{}, errors.New("gate run must be a regular file")
	}
	f, err := openManifest(filename)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, errors.New("gate run must be a regular file")
	}
	if !os.SameFile(initial, info) {
		return Manifest{}, errors.New("gate run changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(raw) > MaxManifestBytes {
		return Manifest{}, errors.New("gate run exceeds maximum size")
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("decode gate run: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return m, errors.New("gate run contains trailing data")
	}
	id := m.RunID
	m.RunID = ""
	validDigest := id == digest(m)
	m.RunID = id
	if m.SchemaVersion != 1 || len(m.Nonce) != 32 || !validDigest || len(m.Gates) != len(spec.ProjectGateOrder) {
		return m, errors.New("invalid gate-run version, inventory or digest")
	}
	if m.Inputs.EnvironmentAssurance != "" && (m.Inputs.EnvironmentAssurance != EnvironmentAssuranceCallerAsserted || m.Inputs.EnvironmentID == "") {
		return m, errors.New("unsupported gate-run environment assurance")
	}
	seen := map[string]bool{}
	for _, gate := range m.Gates {
		if !spec.IsProjectGate(gate.ID) || gate.Definition.ID != gate.ID || seen[gate.ID] {
			return m, errors.New("invalid gate-run gate inventory")
		}
		if err := gate.Definition.Validate(); err != nil {
			return m, err
		}
		seen[gate.ID] = true
	}
	return m, nil
}

// Write never overwrites a caller's record or writes inside the source worktree.
func Write(repo, filename string, m Manifest) error {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	contained, err := pathguard.Contains(repo, abs)
	if err != nil {
		return err
	}
	if contained {
		return errors.New("gate-run output must be outside target worktree")
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > MaxManifestBytes {
		return errors.New("gate run exceeds maximum size")
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(abs)
		return errors.Join(writeErr, closeErr)
	}
	return nil
}

func identity(in Inputs, gate spec.GatePolicy, dependencies map[string]string) Identity {
	var environment any
	if gate.Command != nil {
		environment = gate.Command.Environment
	}
	categories := map[string]string{
		"source":        digest([]string{in.Repository, in.Head, in.SourceSHA256}),
		"contract":      digest(in.ContractSHA256),
		"baseline":      digest(in.Baseline),
		"policy":        in.PolicySHA256,
		"command":       digest(gate),
		"environment":   digest([]any{environment, in.EnvironmentID}),
		"dependencies":  digest(dependencies),
		"polis_version": digest([]string{in.POLISVersion, in.Runtime}),
	}
	return Identity{SHA256: digest(categories), Categories: categories}
}

func differences(old, current Identity) []string {
	if old.SHA256 == "" || len(old.Categories) != len(current.Categories) || old.SHA256 != digest(old.Categories) {
		return []string{"identity_missing_or_invalid"}
	}
	var changed []string
	for key, value := range current.Categories {
		if old.Categories[key] != value {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

// scopedIdentity is opt-in only. A different category inventory prevents a
// previously recorded global identity from being mistaken for a scoped proof.
func scopedIdentity(in Inputs, gate spec.GatePolicy, dependencies map[string]string, sourceSHA256 string) Identity {
	id := identity(in, gate, dependencies)
	id.Categories["source"] = digest([]string{in.Repository, sourceSHA256})
	id.Categories["source_scope"] = digest([]any{"input-paths-complete-v1", gate.InputPaths})
	id.SHA256 = digest(id.Categories)
	return id
}
