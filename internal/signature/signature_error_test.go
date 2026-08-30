package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignFileRejectsInvalidInputsAndExistingOutput(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	privatePath, _ := writeTestKeyPair(t, dir)

	if _, err := SignFile(artifact, privatePath, ""); err == nil || !strings.Contains(err.Error(), "output path") {
		t.Fatalf("empty output error = %v", err)
	}
	if _, err := SignFile(filepath.Join(dir, "missing.polis"), privatePath, filepath.Join(dir, "missing.sig")); err == nil {
		t.Fatal("expected missing artifact rejection")
	}
	badKey := filepath.Join(dir, "bad-private.pem")
	if err := os.WriteFile(badKey, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(artifact, badKey, filepath.Join(dir, "bad.sig")); err == nil || !strings.Contains(err.Error(), "exactly one PEM block") {
		t.Fatalf("bad private key error = %v", err)
	}

	out := filepath.Join(dir, "existing.sig")
	if err := os.WriteFile(out, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(artifact, privatePath, out); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output error = %v", err)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "keep" {
		t.Fatalf("existing output mutated: %q err=%v", got, err)
	}
}

func TestVerifyRejectsSignatureContractViolations(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	privatePath, publicPath := writeTestKeyPair(t, dir)
	baseSignature := filepath.Join(dir, "artifact.polis.sig")
	if _, err := SignFile(artifact, privatePath, baseSignature); err != nil {
		t.Fatal(err)
	}
	base := readSignatureDocument(t, baseSignature)

	cases := []struct {
		name string
		edit func(*Document)
		want string
	}{
		{"schema", func(d *Document) { d.SchemaVersion++ }, "unsupported signature schema_version"},
		{"algorithm", func(d *Document) { d.Algorithm = "other" }, "unsupported signature algorithm"},
		{"artifact digest", func(d *Document) { d.ArtifactSHA256 = strings.Repeat("0", 64) }, "artifact digest mismatch"},
		{"invalid base64", func(d *Document) { d.SignatureBase64 = "%%%" }, "invalid signature bytes"},
		{"wrong signature size", func(d *Document) { d.SignatureBase64 = base64.StdEncoding.EncodeToString([]byte("short")) }, "invalid signature bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := base
			tc.edit(&doc)
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".sig")
			writeSignatureDocument(t, path, doc)
			if err := VerifyFile(artifact, path, publicPath); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifyFile() error = %v, want %q", err, tc.want)
			}
		})
	}

	_, otherPublic := writeTestKeyPair(t, filepath.Join(dir, "other"))
	if err := VerifyFile(artifact, baseSignature, otherPublic); err == nil || !strings.Contains(err.Error(), "trusted public key") {
		t.Fatalf("wrong public key error = %v", err)
	}

	doc := base
	sig, err := base64.StdEncoding.DecodeString(doc.SignatureBase64)
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 0xff
	doc.SignatureBase64 = base64.StdEncoding.EncodeToString(sig)
	corrupt := filepath.Join(dir, "corrupt.sig")
	writeSignatureDocument(t, corrupt, doc)
	if err := VerifyFile(artifact, corrupt, publicPath); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("corrupt signature error = %v", err)
	}
}

func TestKeyLoadersRejectWrongKeyTypesAndMalformedPEM(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "rsa-private.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(artifact, privatePath, filepath.Join(dir, "rsa.sig")); err == nil || !strings.Contains(err.Error(), "not Ed25519") {
		t.Fatalf("RSA private key error = %v", err)
	}

	publicDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(dir, "rsa-public.pem")
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	validSignature := filepath.Join(dir, "valid.sig")
	privForSig, _ := writeTestKeyPair(t, filepath.Join(dir, "signer"))
	if _, err := SignFile(artifact, privForSig, validSignature); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, validSignature, publicPath); err == nil || !strings.Contains(err.Error(), "not Ed25519") {
		t.Fatalf("RSA public key error = %v", err)
	}

	badPublic := filepath.Join(dir, "bad-public.pem")
	if err := os.WriteFile(badPublic, []byte("not pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, validSignature, badPublic); err == nil || !strings.Contains(err.Error(), "exactly one PEM block") {
		t.Fatalf("bad public key error = %v", err)
	}
}

func TestSignatureFileBoundariesAndDocumentParsing(t *testing.T) {
	dir := t.TempDir()
	privatePath, publicPath := writeTestKeyPair(t, dir)
	directoryArtifact := filepath.Join(dir, "artifact-dir")
	if err := os.Mkdir(directoryArtifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(directoryArtifact, privatePath, filepath.Join(dir, "dir.sig")); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory artifact error = %v", err)
	}

	oversized := filepath.Join(dir, "oversized.polis")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxArtifactBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(oversized, privatePath, filepath.Join(dir, "oversized.sig")); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("oversized artifact error = %v", err)
	}

	artifact := filepath.Join(dir, "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(dir, "malformed.sig")
	if err := os.WriteFile(malformed, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, malformed, publicPath); err == nil || !strings.Contains(err.Error(), "decode signature") {
		t.Fatalf("malformed signature error = %v", err)
	}
	unknown := filepath.Join(dir, "unknown.sig")
	if err := os.WriteFile(unknown, []byte(`{"schema_version":1,"algorithm":"ed25519-sha256-v1","artifact_sha256":"x","public_key_sha256":"y","signature_base64":"z","unknown":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, unknown, publicPath); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}

	keyDir := filepath.Join(dir, "key-dir")
	if err := os.Mkdir(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadPrivateKey(keyDir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("private key directory error = %v", err)
	}
	if _, err := loadPublicKey(keyDir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("public key directory error = %v", err)
	}
}

func readSignatureDocument(t *testing.T, path string) Document {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func writeSignatureDocument(t *testing.T, path string, doc Document) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPublicKeyFingerprintIsStableForSameKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first, err := publicKeyFingerprint(pub)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publicKeyFingerprint(pub)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("fingerprints first=%q second=%q", first, second)
	}
}
