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
	for _, version := range []int{LegacyFormatVersion, PreviousFormatVersion} {
		got, err := PackageMembers(version)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, legacy) {
			t.Fatalf("format v%d members=%v want=%v", version, got, legacy)
		}
	}
	got, err := PackageMembers(FormatVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, current) {
		t.Fatalf("format v%d members=%v want=%v", FormatVersion, got, current)
	}
	if _, err := PackageMembers(FormatVersion + 1); err == nil {
		t.Fatal("unsupported package format accepted")
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
