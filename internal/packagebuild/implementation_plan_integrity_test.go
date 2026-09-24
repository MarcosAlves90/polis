package packagebuild

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/signature"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestVerifyRejectsPlanChecksumAndDigestTampering(t *testing.T) {
	artifact := buildPlannedGreenGreenArtifact(t)
	original, headers := readPackageArchive(t, artifact)
	changedPlan := rewritePlanObjective(t, original[spec.MemberImplementationPlan])

	for _, test := range []struct {
		name            string
		updateManifest  bool
		updateChecksums bool
		wantError       string
	}{
		{name: "tampered plan", wantError: "manifest digest mismatch for " + spec.MemberImplementationPlan},
		{name: "manifest digest", updateChecksums: true, wantError: "manifest digest mismatch for " + spec.MemberImplementationPlan},
		{name: "member checksum", updateManifest: true, wantError: "checksum mismatch for \"" + spec.MemberImplementationPlan + "\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			members := clonePackageMembers(original)
			members[spec.MemberImplementationPlan] = changedPlan
			if test.updateManifest {
				members[spec.MemberManifest] = manifestWithPlanDigest(t, members[spec.MemberManifest], changedPlan)
			}
			if test.updateChecksums {
				members[spec.MemberChecksums] = checksumMember(members)
			}
			path := filepath.Join(t.TempDir(), "tampered.polis")
			writePackageArchive(t, path, members, headers)
			if _, err := packageverify.Load(path); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("tampered package error=%v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestDetachedSignatureCoversExactPackagedImplementationPlan(t *testing.T) {
	artifact := buildPlannedGreenGreenArtifact(t)
	privatePath, publicPath := writePlanSignatureKeyPair(t)
	signaturePath := filepath.Join(t.TempDir(), "planned.polis.sig")
	if _, err := signature.SignFile(artifact, privatePath, signaturePath); err != nil {
		t.Fatalf("sign planned package: %v", err)
	}
	members, headers := readPackageArchive(t, artifact)
	members[spec.MemberImplementationPlan] = rewritePlanObjective(t, members[spec.MemberImplementationPlan])
	members[spec.MemberManifest] = manifestWithPlanDigest(t, members[spec.MemberManifest], members[spec.MemberImplementationPlan])
	members[spec.MemberChecksums] = checksumMember(members)
	writePackageArchive(t, artifact, members, headers)
	if _, err := packageverify.Load(artifact); err != nil {
		t.Fatalf("self-consistent modified planned package failed package verification: %v", err)
	}
	if err := signature.VerifyFile(artifact, signaturePath, publicPath); err == nil || !strings.Contains(err.Error(), "signature artifact digest mismatch") {
		t.Fatalf("signature accepted modified plan bytes: %v", err)
	}
}

func TestVerifyRetainsFormatV4UnplannedArtifactCompatibility(t *testing.T) {
	repo, contractPath := strictBehaviorPreservingFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "value.go"), []byte("package polisfixture\nconst preservedValue = 42\nfunc Value() int { return preservedValue }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := Build(context.Background(), Options{
		Repo: repo, Project: "polis", Change: "strict-green-green-v4-compat", Out: t.TempDir(), Contract: contractPath,
	})
	if err != nil {
		t.Fatalf("build unplanned package: %v", err)
	}
	members, headers := readPackageArchive(t, built.Path)
	var manifest spec.Manifest
	if err := json.Unmarshal(members[spec.MemberManifest], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != spec.FormatVersion {
		t.Fatalf("unplanned producer format=%d want=%d", manifest.FormatVersion, spec.FormatVersion)
	}
	manifest.FormatVersion = spec.PreviousFormatVersion
	members[spec.MemberEvidence] = evidenceV2(t, members[spec.MemberEvidence])
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	members[spec.MemberManifest] = append(manifestRaw, '\n')
	members[spec.MemberChecksums] = checksumMember(members)
	formatV4 := filepath.Join(t.TempDir(), "unplanned-v4.polis")
	writePackageArchive(t, formatV4, members, headers)
	verified, err := packageverify.Load(formatV4)
	if err != nil {
		t.Fatalf("historical unplanned format v4 rejected: %v", err)
	}
	if verified.Manifest.FormatVersion != spec.PreviousFormatVersion || verified.ImplementationPlan != nil {
		t.Fatalf("format-v4 plan compatibility=%+v", verified)
	}
	inspection, err := packageverify.Inspect(formatV4)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.ImplementationPlanPresent {
		t.Fatalf("historical format-v4 artifact reports a plan: %+v", inspection)
	}
}

func evidenceV2(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]json.RawMessage
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		delete(event, "deferred_gates")
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func buildPlannedGreenGreenArtifact(t *testing.T) string {
	t.Helper()
	repo, contractPath := strictBehaviorPreservingFixture(t)
	planPath := filepath.Join(t.TempDir(), "implementation-plan.json")
	if _, err := implementationplan.Create(context.Background(), implementationplan.Options{Repo: repo, Contract: contractPath, Out: planPath}); err != nil {
		t.Fatalf("create implementation plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "value.go"), []byte("package polisfixture\nconst preservedValue = 42\nfunc Value() int { return preservedValue }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := Build(context.Background(), Options{
		Repo: repo, Project: "polis", Change: "strict-green-green-integrity", Out: t.TempDir(),
		Contract: contractPath, ImplementationPlan: planPath,
	})
	if err != nil {
		t.Fatalf("build planned Green/Green artifact: %v", err)
	}
	return built.Path
}

func readPackageArchive(t *testing.T, path string) (map[string][]byte, map[string]zip.FileHeader) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	members := make(map[string][]byte, len(r.File))
	headers := make(map[string]zip.FileHeader, len(r.File))
	for _, file := range r.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		members[file.Name] = content
		headers[file.Name] = file.FileHeader
	}
	return members, headers
}

func writePackageArchive(t *testing.T, path string, members map[string][]byte, headers map[string]zip.FileHeader) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header, ok := headers[name]
		if !ok {
			header = zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0o644)
		} else {
			header.CRC32 = 0
			header.CompressedSize = 0
			header.UncompressedSize = 0
			header.CompressedSize64 = 0
			header.UncompressedSize64 = 0
		}
		entry, err := w.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(members[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func clonePackageMembers(source map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(source))
	for name, content := range source {
		clone[name] = append([]byte(nil), content...)
	}
	return clone
}

func rewritePlanObjective(t *testing.T, raw []byte) []byte {
	t.Helper()
	var plan spec.ImplementationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("cannot mutate an empty implementation plan")
	}
	plan.Steps[0].Objective += " revised"
	changed, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(changed, '\n')
}

func manifestWithPlanDigest(t *testing.T, raw, planBytes []byte) []byte {
	t.Helper()
	var manifest spec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(planBytes)
	manifest.ImplementationPlanSHA256 = hex.EncodeToString(sum[:])
	changed, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(changed, '\n')
}

func checksumMember(members map[string][]byte) []byte {
	names := make([]string, 0, len(members)-1)
	for name := range members {
		if name != spec.MemberChecksums {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out bytes.Buffer
	for _, name := range names {
		sum := sha256.Sum256(members[name])
		fmt.Fprintf(&out, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return out.Bytes()
}

func writePlanSignatureKeyPair(t *testing.T) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(t.TempDir(), "private.pem")
	publicPath := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return privatePath, publicPath
}
