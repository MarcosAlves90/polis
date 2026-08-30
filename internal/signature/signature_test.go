package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestDetachedSignatureBindsExactArtifactBytes(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "change.polis")
	if err := os.WriteFile(artifact, []byte("exact artifact bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	privPath, pubPath := writeTestKeyPair(t, dir)
	sigPath := filepath.Join(dir, "change.polis.sig")
	if _, err := SignFile(artifact, privPath, sigPath); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, sigPath, pubPath); err != nil {
		t.Fatalf("verify exact bytes: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("tampered artifact bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, sigPath, pubPath); err == nil {
		t.Fatal("expected tampered artifact to fail")
	}
}

func TestVerifyRejectsTrailingSignatureJSON(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "change.polis")
	privatePath, publicPath := writeTestKeyPair(t, dir)
	signaturePath := filepath.Join(dir, "change.polis.sig")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SignFile(artifact, privatePath, signaturePath); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(signaturePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n{}\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(artifact, signaturePath, publicPath); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func writeTestKeyPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	privPath, pubPath := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	return privPath, pubPath
}
