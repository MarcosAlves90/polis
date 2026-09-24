package packageverify

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/baselineproof"
	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	MaxArchiveBytes           = spec.MaxArchiveBytes
	MaxTotalUncompressedBytes = spec.MaxTotalUncompressedBytes
	MaxContractMemberBytes    = spec.MaxContractMemberBytes
	MaxEvidenceMemberBytes    = spec.MaxEvidenceMemberBytes
	MaxPatchMemberBytes       = spec.MaxPatchMemberBytes
	MaxBaselineMemberBytes    = spec.MaxBaselineMemberBytes
	MaxChecksumsMemberBytes   = spec.MaxChecksumsMemberBytes

	memberBaseline           = spec.MemberBaseline
	memberChange             = spec.MemberChange
	memberChecksums          = spec.MemberChecksums
	memberEvidence           = spec.MemberEvidence
	memberImplementationPlan = spec.MemberImplementationPlan
	memberManifest           = spec.MemberManifest
	memberPayload            = spec.MemberPayload
	memberPolicy             = spec.MemberPolicy
	memberRegression         = spec.MemberRegression
)

var lowerSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Result struct {
	Project                    string
	Change                     string
	BaseCommit                 string
	TargetTree                 string
	ValidationLevel            string
	EnabledGates               []string
	DisabledGates              []string
	DeferredGates              []string
	ConsumerValidationRequired bool
}

type Inspection struct {
	Project                         string                           `json:"project"`
	Change                          string                           `json:"change"`
	FormatVersion                   int                              `json:"format_version"`
	PolicySchemaVersion             int                              `json:"policy_schema_version"`
	ValidationLevel                 string                           `json:"validation_level"`
	EnabledGates                    []string                         `json:"enabled_gates"`
	DisabledGates                   []string                         `json:"disabled_gates"`
	DeferredGates                   []string                         `json:"deferred_gates"`
	ConsumerValidationRequired      bool                             `json:"consumer_validation_required"`
	ChangeContractSchemaVersion     int                              `json:"change_contract_schema_version"`
	Commit                          *spec.CommitMetadata             `json:"commit,omitempty"`
	Kind                            string                           `json:"kind"`
	BaseCommit                      string                           `json:"base_commit"`
	TargetTree                      string                           `json:"target_tree"`
	AllowedPaths                    []string                         `json:"allowed_paths"`
	Gates                           []string                         `json:"gates"`
	EvidenceEvents                  int                              `json:"evidence_events"`
	Traceability                    []spec.TraceabilityLink          `json:"traceability,omitempty"`
	ImplementationPlanPresent       bool                             `json:"implementation_plan_present"`
	ImplementationPlanSchemaVersion int                              `json:"implementation_plan_schema_version,omitempty"`
	ImplementationPlanStrategy      string                           `json:"implementation_plan_strategy,omitempty"`
	ImplementationPlanStepCount     int                              `json:"implementation_plan_step_count,omitempty"`
	ImplementationPlanTraceability  []ImplementationPlanTraceability `json:"implementation_plan_traceability,omitempty"`
}

type ImplementationPlanTraceability struct {
	RequirementID         string   `json:"requirement_id"`
	AcceptanceCriterionID string   `json:"acceptance_criterion_id"`
	Proof                 string   `json:"proof"`
	PlanStepIDs           []string `json:"plan_step_ids"`
}

type Package struct {
	Result                Result
	Manifest              spec.Manifest
	Policy                spec.Policy
	Change                spec.ChangeContract
	Patch                 []byte
	RegressionPatch       []byte
	Evidence              []byte
	Baseline              []byte
	ImplementationPlan    *spec.ImplementationPlan
	ImplementationPlanRaw []byte
}

type decodedContracts struct {
	manifest spec.Manifest
	policy   spec.Policy
	change   spec.ChangeContract
}

func Verify(filename string) (Result, error) {
	pkg, err := Load(filename)
	if err != nil {
		return Result{}, err
	}
	return pkg.Result, nil
}

func Inspect(filename string) (Inspection, error) {
	pkg, err := Load(filename)
	if err != nil {
		return Inspection{}, err
	}
	evidenceVersion, err := spec.EvidenceVersionForFormat(pkg.Manifest.FormatVersion)
	if err != nil {
		return Inspection{}, err
	}
	events, err := spec.DecodeEvidenceVersion(pkg.Evidence, evidenceVersion)
	if err != nil {
		return Inspection{}, err
	}
	inspection := Inspection{
		Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, FormatVersion: pkg.Manifest.FormatVersion,
		PolicySchemaVersion: pkg.Policy.SchemaVersion, ValidationLevel: pkg.Policy.EffectiveValidationLevel(), ChangeContractSchemaVersion: pkg.Change.SchemaVersion,
		Kind: pkg.Change.Kind, BaseCommit: pkg.Manifest.BaseCommit, TargetTree: pkg.Manifest.TargetTree,
		EvidenceEvents: len(events),
	}
	if pkg.Change.Commit != nil {
		commit := *pkg.Change.Commit
		inspection.Commit = &commit
	}
	summary := pkg.Policy.ValidationSummary()
	inspection.EnabledGates = append([]string(nil), summary.EnabledGates...)
	inspection.DisabledGates = append([]string(nil), summary.DisabledGates...)
	inspection.DeferredGates = append([]string{}, pkg.Result.DeferredGates...)
	inspection.ConsumerValidationRequired = pkg.Result.ConsumerValidationRequired
	if pkg.Change.Scope != nil {
		inspection.AllowedPaths = append([]string(nil), pkg.Change.Scope.AllowedPaths...)
	} else {
		inspection.AllowedPaths = []string{"."}
	}
	inspection.Traceability = traceabilityForChange(pkg.Change)
	if pkg.ImplementationPlan != nil {
		inspection.ImplementationPlanPresent = true
		inspection.ImplementationPlanSchemaVersion = pkg.ImplementationPlan.SchemaVersion
		inspection.ImplementationPlanStrategy = pkg.ImplementationPlan.Strategy
		inspection.ImplementationPlanStepCount = len(pkg.ImplementationPlan.Steps)
		inspection.ImplementationPlanTraceability = traceabilityForImplementationPlan(pkg.Change, *pkg.ImplementationPlan)
	}
	inspection.Gates = make([]string, 0, len(pkg.Policy.Gates))
	for _, gate := range pkg.Policy.Gates {
		inspection.Gates = append(inspection.Gates, gate.ID)
	}
	return inspection, nil
}

func traceabilityForChange(change spec.ChangeContract) []spec.TraceabilityLink {
	if change.Specification == nil {
		return nil
	}
	return change.Specification.TraceabilityLinks()
}

func traceabilityForImplementationPlan(change spec.ChangeContract, plan spec.ImplementationPlan) []ImplementationPlanTraceability {
	links := traceabilityForChange(change)
	traceability := make([]ImplementationPlanTraceability, 0, len(links))
	for _, link := range links {
		entry := ImplementationPlanTraceability{
			RequirementID: link.RequirementID, AcceptanceCriterionID: link.AcceptanceCriterionID, Proof: link.Proof,
			PlanStepIDs: make([]string, 0),
		}
		for _, step := range plan.Steps {
			if containsString(step.Requirements, link.RequirementID) && containsString(step.AcceptanceCriteria, link.AcceptanceCriterionID) {
				entry.PlanStepIDs = append(entry.PlanStepIDs, step.ID)
			}
		}
		traceability = append(traceability, entry)
	}
	return traceability
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func Load(filename string) (Package, error) {
	contents, err := loadArchiveContents(filename)
	if err != nil {
		return Package{}, err
	}
	contracts, err := decodeContracts(contents)
	if err != nil {
		return Package{}, err
	}
	if err := validateChangeContractFormatCompatibility(contracts.manifest.FormatVersion, contracts.change.SchemaVersion); err != nil {
		return Package{}, err
	}
	if err := validateInventory(contents, contracts.manifest.FormatVersion); err != nil {
		return Package{}, err
	}
	regressionPatch := contents[memberRegression]
	if err := validateRegressionPatch(contracts.change, regressionPatch); err != nil {
		return Package{}, err
	}
	deferredGates, err := validateEvidenceAndIntegrity(contents, contracts)
	if err != nil {
		return Package{}, err
	}
	implementationPlan, implementationPlanRaw, err := validateImplementationPlan(contents, contracts)
	if err != nil {
		return Package{}, err
	}
	if err := verifyLockedDevelopmentBaseline(contracts.manifest, contracts.change, contents); err != nil {
		return Package{}, err
	}
	if err := verifyEmbeddedBaseline(contracts.manifest, contracts.change, contents); err != nil {
		return Package{}, err
	}
	return packageFromContents(contents, contracts, regressionPatch, deferredGates, implementationPlan, implementationPlanRaw), nil
}

func validateChangeContractFormatCompatibility(formatVersion, changeSchemaVersion int) error {
	if changeSchemaVersion == spec.CommitIntentDraftChangeContractSchemaVersion {
		return errors.New("package cannot contain draft Change Contract schema v5")
	}
	if changeSchemaVersion == spec.CommitIntentLockedChangeContractSchemaVersion && formatVersion != spec.FormatVersion && formatVersion != spec.ImplementationPlanFormatVersion {
		return fmt.Errorf("locked Change Contract schema v6 requires package format v%d or v%d", spec.FormatVersion, spec.ImplementationPlanFormatVersion)
	}
	return nil
}

func loadArchiveContents(filename string) (map[string][]byte, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("stat POLIS archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("POLIS archive must be a regular file")
	}
	if info.Size() > MaxArchiveBytes {
		return nil, fmt.Errorf("POLIS archive exceeds maximum size %d", MaxArchiveBytes)
	}
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open POLIS archive: %w", err)
	}
	defer zr.Close()
	files, err := indexArchiveFiles(zr.File)
	if err != nil {
		return nil, err
	}
	return readArchiveContents(files)
}

func indexArchiveFiles(entries []*zip.File) (map[string]*zip.File, error) {
	files := make(map[string]*zip.File, len(entries))
	var total uint64
	for _, f := range entries {
		if err := validateArchiveFile(f, files); err != nil {
			return nil, err
		}
		max, ok := memberLimit(f.Name)
		if !ok {
			return nil, fmt.Errorf("unexpected archive member %q", f.Name)
		}
		if f.UncompressedSize64 > max {
			return nil, fmt.Errorf("archive member %q exceeds maximum uncompressed size %d", f.Name, max)
		}
		total += f.UncompressedSize64
		if total > MaxTotalUncompressedBytes {
			return nil, fmt.Errorf("archive exceeds maximum total uncompressed size %d", MaxTotalUncompressedBytes)
		}
		files[f.Name] = f
	}
	return files, nil
}

func memberLimit(name string) (uint64, bool) {
	return spec.PackageMemberLimit(name)
}

func validateArchiveFile(f *zip.File, indexed map[string]*zip.File) error {
	if err := validateMemberPath(f.Name); err != nil {
		return err
	}
	if !f.Mode().IsRegular() {
		return fmt.Errorf("archive member %q is not a regular file", f.Name)
	}
	if _, exists := indexed[f.Name]; exists {
		return fmt.Errorf("duplicate archive member %q", f.Name)
	}
	return nil
}

func readArchiveContents(files map[string]*zip.File) (map[string][]byte, error) {
	contents := make(map[string][]byte, len(files))
	for name, f := range files {
		b, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		contents[name] = b
	}
	return contents, nil
}

func decodeContracts(contents map[string][]byte) (decodedContracts, error) {
	manifest, err := spec.DecodeManifest(contents[memberManifest])
	if err != nil {
		return decodedContracts{}, err
	}
	policy, err := spec.DecodePolicy(contents[memberPolicy])
	if err != nil {
		return decodedContracts{}, err
	}
	change, err := spec.DecodeChangeContract(contents[memberChange])
	if err != nil {
		return decodedContracts{}, err
	}
	return decodedContracts{manifest: manifest, policy: policy, change: change}, nil
}

func verifyLockedDevelopmentBaseline(manifest spec.Manifest, change spec.ChangeContract, contents map[string][]byte) error {
	if !change.IsLockedStrictDevelopment() {
		return nil
	}
	if change.BaselineLock == nil || change.Specification == nil {
		return errors.New("locked change contract is missing baseline lock or specification")
	}
	lock := change.BaselineLock
	if lock.GitObjectFormat != manifest.GitObjectFormat {
		return errors.New("baseline_lock git_object_format does not match manifest")
	}
	if lock.BaseCommit != manifest.BaseCommit {
		return errors.New("baseline_lock base_commit does not match manifest")
	}
	policySum := sha256.Sum256(contents[memberPolicy])
	if hex.EncodeToString(policySum[:]) != lock.PolicySHA256 {
		return errors.New("baseline_lock policy_sha256 does not match packaged policy")
	}
	specificationSum, err := change.Specification.SHA256()
	if err != nil {
		return fmt.Errorf("hash packaged specification: %w", err)
	}
	if specificationSum != lock.SpecificationSHA256 {
		return errors.New("baseline_lock specification_sha256 does not match packaged specification")
	}
	return nil
}

func verifyEmbeddedBaseline(manifest spec.Manifest, change spec.ChangeContract, contents map[string][]byte) error {
	if !spec.FormatHasEmbeddedBaseline(manifest.FormatVersion) {
		return nil
	}
	if change.BaselineLock == nil {
		return fmt.Errorf("format v%d package requires baseline_lock", manifest.FormatVersion)
	}
	if err := baselineproof.Verify(contents[memberBaseline], manifest.GitObjectFormat, manifest.BaseCommit, change.BaselineLock.BaseTree); err != nil {
		return fmt.Errorf("verify embedded baseline: %w", err)
	}
	return nil
}

func validateRegressionPatch(change spec.ChangeContract, regressionPatch []byte) error {
	if change.RequiresRedGreen() && len(regressionPatch) == 0 {
		if change.Kind == spec.ChangeKindDefect {
			return errors.New("defect package requires non-empty regression patch")
		}
		return errors.New("red_green feature package requires non-empty regression patch")
	}
	if !change.RequiresRedGreen() && len(regressionPatch) != 0 {
		return errors.New("package without red_green requires empty regression patch")
	}
	return nil
}

func validateEvidenceAndIntegrity(contents map[string][]byte, contracts decodedContracts) ([]string, error) {
	evidenceVersion, err := spec.EvidenceVersionForFormat(contracts.manifest.FormatVersion)
	if err != nil {
		return nil, err
	}
	events, err := spec.DecodeEvidenceVersion(contents[memberEvidence], evidenceVersion)
	if err != nil {
		return nil, err
	}
	if contracts.manifest.FormatVersion >= spec.IntermediateFormatVersion {
		for i, event := range events {
			if event.Event == "command_finished" && (event.Stdout != nil || event.Stderr != nil) {
				return nil, fmt.Errorf("evidence event %d stores raw command output in package format v3+", i)
			}
		}
	}
	if err := spec.ValidatePassEvidenceForVersion(events, contracts.change, contracts.policy, evidenceVersion); err != nil {
		return nil, fmt.Errorf("validate evidence contract: %w", err)
	}
	if err := verifyManifestDigests(contracts.manifest, contents); err != nil {
		return nil, err
	}
	if err := verifyChecksumFile(contents); err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.Event == "validation_configured" && event.DeferredGates != nil {
			return append([]string{}, event.DeferredGates...), nil
		}
	}
	return []string{}, nil
}

func packageFromContents(contents map[string][]byte, contracts decodedContracts, regressionPatch []byte, deferredGates []string, implementationPlan *spec.ImplementationPlan, implementationPlanRaw []byte) Package {
	manifest := contracts.manifest
	summary := contracts.policy.ValidationSummary()
	result := Result{
		Project: manifest.Project, Change: manifest.Change, BaseCommit: manifest.BaseCommit, TargetTree: manifest.TargetTree,
		ValidationLevel: summary.Level, EnabledGates: append([]string{}, summary.EnabledGates...), DisabledGates: append([]string{}, summary.DisabledGates...),
		DeferredGates: append([]string{}, deferredGates...), ConsumerValidationRequired: len(deferredGates) > 0,
	}
	return Package{
		Result: result, Manifest: manifest, Policy: contracts.policy, Change: contracts.change,
		Patch:                 append([]byte(nil), contents[memberPayload]...),
		RegressionPatch:       append([]byte(nil), regressionPatch...),
		Evidence:              append([]byte(nil), contents[memberEvidence]...),
		Baseline:              append([]byte(nil), contents[memberBaseline]...),
		ImplementationPlan:    implementationPlan,
		ImplementationPlanRaw: append([]byte(nil), implementationPlanRaw...),
	}
}

func validateImplementationPlan(contents map[string][]byte, contracts decodedContracts) (*spec.ImplementationPlan, []byte, error) {
	if contracts.manifest.FormatVersion != spec.ImplementationPlanFormatVersion {
		return nil, nil, nil
	}
	raw := contents[memberImplementationPlan]
	plan, err := spec.DecodeImplementationPlan(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid packaged implementation plan: %w", err)
	}
	if err := plan.ValidateAgainst(contracts.change, contents[memberChange]); err != nil {
		return nil, nil, fmt.Errorf("invalid packaged implementation plan: %w", err)
	}
	execution, err := policyplan.Compile(contracts.policy)
	if err != nil {
		return nil, nil, fmt.Errorf("compile packaged Project Policy for implementation plan: %w", err)
	}
	gateOrder, err := implementationplan.EffectiveExecutionOrder(execution)
	if err != nil {
		return nil, nil, err
	}
	if err := plan.ValidateProjectGates(gateOrder); err != nil {
		return nil, nil, fmt.Errorf("invalid packaged implementation plan: %w", err)
	}
	return &plan, append([]byte(nil), raw...), nil
}

func validateMemberPath(name string) error {
	if name == "" {
		return errors.New("empty archive member path")
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("archive member %q contains backslash", name)
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") {
		return fmt.Errorf("archive member %q is absolute", name)
	}
	if hasDriveLetterPrefix(name) {
		return fmt.Errorf("archive member %q uses drive-letter path", name)
	}
	if path.Clean(name) != name {
		return fmt.Errorf("archive member %q is not normalized", name)
	}
	if hasProhibitedPathSegment(name) {
		return fmt.Errorf("archive member %q contains prohibited path segment", name)
	}
	if !strings.HasPrefix(name, "polis/") {
		return fmt.Errorf("archive member %q is outside polis root", name)
	}
	return nil
}

func hasDriveLetterPrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	return (name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')
}

func hasProhibitedPathSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func validateInventory(contents map[string][]byte, formatVersion int) error {
	expected, err := spec.PackageMembers(formatVersion)
	if err != nil {
		return err
	}
	if len(contents) != len(expected) {
		return fmt.Errorf("invalid archive inventory: got %d members, want %d for format v%d", len(contents), len(expected), formatVersion)
	}
	for _, name := range expected {
		if _, ok := contents[name]; !ok {
			return fmt.Errorf("missing required archive member %q", name)
		}
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	max, ok := memberLimit(f.Name)
	if !ok {
		return nil, fmt.Errorf("unexpected archive member %q", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) > max {
		return nil, fmt.Errorf("archive member %q exceeds maximum size %d", f.Name, max)
	}
	return b, nil
}

func verifyManifestDigests(m spec.Manifest, contents map[string][]byte) error {
	checks := []struct{ name, want string }{
		{memberPolicy, m.PolicySHA256},
		{memberChange, m.ChangeContractSHA256},
		{memberRegression, m.RegressionPatchSHA256},
		{memberPayload, m.PayloadSHA256},
	}
	if spec.FormatHasEmbeddedBaseline(m.FormatVersion) {
		checks = append(checks, struct{ name, want string }{memberBaseline, m.BaselineSHA256})
	}
	if m.FormatVersion == spec.ImplementationPlanFormatVersion {
		checks = append(checks, struct{ name, want string }{memberImplementationPlan, m.ImplementationPlanSHA256})
	}
	for _, check := range checks {
		if err := verifyDigest(contents[check.name], check.want, check.name); err != nil {
			return err
		}
	}
	return nil
}

func verifyDigest(data []byte, want, name string) error {
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("manifest digest mismatch for %s", name)
	}
	return nil
}

func verifyChecksumFile(contents map[string][]byte) error {
	expected := expectedChecksumNames(contents)
	gotNames, err := scanChecksumEntries(contents, expected)
	if err != nil {
		return err
	}
	return validateChecksumInventory(gotNames, expected)
}

func expectedChecksumNames(contents map[string][]byte) []string {
	expected := make([]string, 0, len(contents)-1)
	for name := range contents {
		if name != memberChecksums {
			expected = append(expected, name)
		}
	}
	sort.Strings(expected)
	return expected
}

func scanChecksumEntries(contents map[string][]byte, expected []string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(contents[memberChecksums])))
	gotNames := make([]string, 0, len(expected))
	seen := map[string]bool{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		name, err := verifyChecksumLine(scanner.Text(), lineNo, contents, seen)
		if err != nil {
			return nil, err
		}
		seen[name] = true
		gotNames = append(gotNames, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksum file: %w", err)
	}
	return gotNames, nil
}

func verifyChecksumLine(line string, lineNo int, contents map[string][]byte, seen map[string]bool) (string, error) {
	digest, name, err := parseChecksumLine(line, lineNo)
	if err != nil {
		return "", err
	}
	if err := validateChecksumName(name, lineNo, seen); err != nil {
		return "", err
	}
	data, ok := contents[name]
	if !ok {
		return "", fmt.Errorf("checksum references unknown member %q", name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return "", fmt.Errorf("checksum mismatch for %q", name)
	}
	return name, nil
}

func parseChecksumLine(line string, lineNo int) (string, string, error) {
	if len(line) < 67 || line[64:66] != "  " {
		return "", "", fmt.Errorf("malformed checksum line %d", lineNo)
	}
	digest, name := line[:64], line[66:]
	if !lowerSHA256.MatchString(digest) {
		return "", "", fmt.Errorf("malformed checksum digest on line %d", lineNo)
	}
	return digest, name, nil
}

func validateChecksumName(name string, lineNo int, seen map[string]bool) error {
	if err := validateMemberPath(name); err != nil {
		return fmt.Errorf("invalid checksum path on line %d: %w", lineNo, err)
	}
	if name == memberChecksums {
		return errors.New("checksum file must not hash itself")
	}
	if seen[name] {
		return fmt.Errorf("duplicate checksum entry %q", name)
	}
	return nil
}

func validateChecksumInventory(gotNames, expected []string) error {
	if len(gotNames) != len(expected) {
		return fmt.Errorf("checksum inventory mismatch: got %d entries, want %d", len(gotNames), len(expected))
	}
	if !sort.StringsAreSorted(gotNames) {
		return errors.New("checksum entries are not sorted")
	}
	for i := range expected {
		if gotNames[i] != expected[i] {
			return fmt.Errorf("checksum inventory mismatch at %d: got %q want %q", i, gotNames[i], expected[i])
		}
	}
	return nil
}
