package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignatureRejectsMalformedKeyMaterialAndExternalPaths(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}

	malformedPrivate := filepath.Join(dir, "malformed-private.pem")
	if err := os.WriteFile(malformedPrivate, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad-der")}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(artifact, malformedPrivate, filepath.Join(dir, "bad.sig")); err == nil || !strings.Contains(err.Error(), "parse PKCS#8") {
		t.Fatalf("error=%v", err)
	}

	privatePath, _ := writeTestKeyPair(t, filepath.Join(dir, "signer"))
	validSig := filepath.Join(dir, "valid.sig")
	if _, err := SignFile(artifact, privatePath, validSig); err != nil {
		t.Fatal(err)
	}
	malformedPublic := filepath.Join(dir, "malformed-public.pem")
	if err := os.WriteFile(malformedPublic, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("bad-der")}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, validSig, malformedPublic); err == nil || !strings.Contains(err.Error(), "parse PKIX") {
		t.Fatalf("error=%v", err)
	}

	if err := VerifyFile(filepath.Join(dir, "missing.polis"), validSig, malformedPublic); err == nil || !strings.Contains(err.Error(), "open artifact") {
		t.Fatalf("missing artifact error=%v", err)
	}
	if _, err := SignFile(artifact, privatePath, filepath.Join(dir, "missing-parent", "out.sig")); err == nil || !strings.Contains(err.Error(), "create signature") {
		t.Fatalf("create error=%v", err)
	}
}

func TestSignatureRejectsOversizedAndTrailingDocuments(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	privatePath, publicPath := writeTestKeyPair(t, dir)
	validSig := filepath.Join(dir, "valid.sig")
	if _, err := SignFile(artifact, privatePath, validSig); err != nil {
		t.Fatal(err)
	}

	oversized := filepath.Join(dir, "oversized.sig")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSignatureBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, oversized, publicPath); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("oversized signature error=%v", err)
	}

	raw, err := os.ReadFile(validSig)
	if err != nil {
		t.Fatal(err)
	}
	trailing := filepath.Join(dir, "trailing.sig")
	if err := os.WriteFile(trailing, append(raw, []byte("not-json")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, trailing, publicPath); err == nil || !strings.Contains(err.Error(), "decode trailing signature data") {
		t.Fatalf("trailing error=%v", err)
	}
}

func TestReadLimitedRejectsMissingAndOversizedKeys(t *testing.T) {
	if _, _, err := loadPrivateKey(filepath.Join(t.TempDir(), "missing.pem")); err == nil || !strings.Contains(err.Error(), "open private key") {
		t.Fatalf("missing private key error=%v", err)
	}

	dir := t.TempDir()
	oversized := filepath.Join(dir, "oversized-key.pem")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxKeyBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadPrivateKey(oversized); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("oversized private key error=%v", err)
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("key generation err=%v", err)
	}
}
