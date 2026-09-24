package spec

import "fmt"

const (
	MemberBaseline           = "polis/polis-baseline.tar"
	MemberChange             = "polis/polis-change.json"
	MemberChecksums          = "polis/polis-checksums.sha256"
	MemberEvidence           = "polis/polis-evidence.ndjson"
	MemberImplementationPlan = "polis/polis-implementation-plan.json"
	MemberManifest           = "polis/polis-manifest.json"
	MemberPayload            = "polis/polis-payload.patch"
	MemberPolicy             = "polis/polis-policy.json"
	MemberRegression         = "polis/polis-regression.patch"
)

const (
	MaxArchiveBytes           int64  = 64 << 20
	MaxTotalUncompressedBytes uint64 = 64 << 20
	MaxContractMemberBytes    uint64 = 1 << 20
	MaxEvidenceMemberBytes    uint64 = 16 << 20
	MaxPatchMemberBytes       uint64 = 32 << 20
	MaxBaselineMemberBytes    uint64 = 32 << 20
	MaxChecksumsMemberBytes   uint64 = 64 << 10
)

var legacyPackageMembers = []string{
	MemberChange,
	MemberChecksums,
	MemberEvidence,
	MemberManifest,
	MemberPayload,
	MemberPolicy,
	MemberRegression,
}

var currentPackageMembers = []string{
	MemberBaseline,
	MemberChange,
	MemberChecksums,
	MemberEvidence,
	MemberManifest,
	MemberPayload,
	MemberPolicy,
	MemberRegression,
}

var plannedPackageMembers = []string{
	MemberBaseline,
	MemberChange,
	MemberChecksums,
	MemberEvidence,
	MemberImplementationPlan,
	MemberManifest,
	MemberPayload,
	MemberPolicy,
	MemberRegression,
}

func PackageMembers(formatVersion int) ([]string, error) {
	switch formatVersion {
	case LegacyFormatVersion, IntermediateFormatVersion:
		return append([]string(nil), legacyPackageMembers...), nil
	case PreviousFormatVersion, FormatVersion:
		return append([]string(nil), currentPackageMembers...), nil
	case ImplementationPlanFormatVersion:
		return append([]string(nil), plannedPackageMembers...), nil
	default:
		return nil, fmt.Errorf("unsupported format_version %d", formatVersion)
	}
}

func FormatHasEmbeddedBaseline(formatVersion int) bool {
	return formatVersion == PreviousFormatVersion || formatVersion == FormatVersion || formatVersion == ImplementationPlanFormatVersion
}

func EvidenceVersionForFormat(formatVersion int) (EvidenceVersion, error) {
	switch formatVersion {
	case LegacyFormatVersion, IntermediateFormatVersion, PreviousFormatVersion:
		return EvidenceVersionV2, nil
	case FormatVersion, ImplementationPlanFormatVersion:
		return EvidenceVersionV3, nil
	default:
		return 0, fmt.Errorf("unsupported format_version %d", formatVersion)
	}
}

func PackageMemberLimit(name string) (uint64, bool) {
	switch name {
	case MemberManifest, MemberPolicy, MemberChange:
		return MaxContractMemberBytes, true
	case MemberEvidence:
		return MaxEvidenceMemberBytes, true
	case MemberPayload, MemberRegression:
		return MaxPatchMemberBytes, true
	case MemberBaseline:
		return MaxBaselineMemberBytes, true
	case MemberImplementationPlan:
		return MaxImplementationPlanBytes, true
	case MemberChecksums:
		return MaxChecksumsMemberBytes, true
	default:
		return 0, false
	}
}
