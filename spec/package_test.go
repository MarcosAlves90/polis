package spec

import (
	"reflect"
	"testing"
)

func TestPackageMembersAreVersionSpecificAndExact(t *testing.T) {
	legacy := []string{
		MemberChange,
		MemberChecksums,
		MemberEvidence,
		MemberManifest,
		MemberPayload,
		MemberPolicy,
		MemberRegression,
	}
	current := []string{
		MemberBaseline,
		MemberChange,
		MemberChecksums,
		MemberEvidence,
		MemberManifest,
		MemberPayload,
		MemberPolicy,
		MemberRegression,
	}
	for _, version := range []int{LegacyFormatVersion, IntermediateFormatVersion} {
		got, err := PackageMembers(version)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, legacy) {
			t.Fatalf("format v%d members=%v want=%v", version, got, legacy)
		}
	}
	for _, version := range []int{PreviousFormatVersion, FormatVersion} {
		got, err := PackageMembers(version)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, current) {
			t.Fatalf("format v%d members=%v want=%v", version, got, current)
		}
	}
	if _, err := PackageMembers(ImplementationPlanFormatVersion + 1); err == nil {
		t.Fatal("unsupported package format accepted")
	}
}

func TestImplementationPlanFormatV6AddsExactNinthMember(t *testing.T) {
	want := []string{
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
	got, err := PackageMembers(ImplementationPlanFormatVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planned package members=%v want=%v", got, want)
	}
	version, err := EvidenceVersionForFormat(ImplementationPlanFormatVersion)
	if err != nil || version != EvidenceVersionV3 || !FormatHasEmbeddedBaseline(ImplementationPlanFormatVersion) {
		t.Fatalf("planned package format mapping: evidence=%d baseline=%t err=%v", version, FormatHasEmbeddedBaseline(ImplementationPlanFormatVersion), err)
	}
	if limit, ok := PackageMemberLimit(MemberImplementationPlan); !ok || limit != MaxImplementationPlanBytes {
		t.Fatalf("implementation plan member limit=(%d,%v) want=(%d,true)", limit, ok, MaxImplementationPlanBytes)
	}
}

func TestPackageFormatEvidenceVersionAndBaselineMapping(t *testing.T) {
	for _, version := range []int{LegacyFormatVersion, IntermediateFormatVersion} {
		evidenceVersion, err := EvidenceVersionForFormat(version)
		if err != nil || evidenceVersion != EvidenceVersionV2 || FormatHasEmbeddedBaseline(version) {
			t.Fatalf("format v%d maps to evidence=%d err=%v baseline=%v", version, evidenceVersion, err, FormatHasEmbeddedBaseline(version))
		}
	}
	evidenceVersion, err := EvidenceVersionForFormat(PreviousFormatVersion)
	if err != nil || evidenceVersion != EvidenceVersionV2 || !FormatHasEmbeddedBaseline(PreviousFormatVersion) {
		t.Fatalf("v4 format mapping evidence=%d err=%v baseline=%v", evidenceVersion, err, FormatHasEmbeddedBaseline(PreviousFormatVersion))
	}
	evidenceVersion, err = EvidenceVersionForFormat(FormatVersion)
	if err != nil || evidenceVersion != EvidenceVersionV3 || !FormatHasEmbeddedBaseline(FormatVersion) {
		t.Fatalf("current format mapping evidence=%d err=%v baseline=%v", evidenceVersion, err, FormatHasEmbeddedBaseline(FormatVersion))
	}
}

func TestPackageMemberLimitsIncludeEmbeddedBaseline(t *testing.T) {
	if got, ok := PackageMemberLimit(MemberBaseline); !ok || got != MaxBaselineMemberBytes {
		t.Fatalf("baseline limit=(%d,%v) want=(%d,true)", got, ok, MaxBaselineMemberBytes)
	}
	if MaxBaselineMemberBytes > MaxTotalUncompressedBytes {
		t.Fatalf("baseline member limit %d exceeds aggregate cap %d", MaxBaselineMemberBytes, MaxTotalUncompressedBytes)
	}
}
