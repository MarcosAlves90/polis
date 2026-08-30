package signature

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	SchemaVersion     = 1
	Algorithm         = "ed25519-sha256-v1"
	maxArtifactBytes  = 64 << 20
	maxKeyBytes       = 1 << 20
	maxSignatureBytes = 1 << 20
)

type Document struct {
	SchemaVersion   int    `json:"schema_version"`
	Algorithm       string `json:"algorithm"`
	ArtifactSHA256  string `json:"artifact_sha256"`
	PublicKeySHA256 string `json:"public_key_sha256"`
	SignatureBase64 string `json:"signature_base64"`
}

type Result struct {
	ArtifactSHA256  string
	PublicKeySHA256 string
	SignaturePath   string
}

func SignFile(artifactPath, privateKeyPath, outPath string) (Result, error) {
	if outPath == "" {
		return Result{}, errors.New("signature output path is required")
	}
	digest, err := fileDigest(artifactPath)
	if err != nil {
		return Result{}, err
	}
	privateKey, publicKey, err := loadPrivateKey(privateKeyPath)
	if err != nil {
		return Result{}, err
	}
	publicFingerprint, err := publicKeyFingerprint(publicKey)
	if err != nil {
		return Result{}, err
	}
	sig := ed25519.Sign(privateKey, digest[:])
	doc := Document{
		SchemaVersion:   SchemaVersion,
		Algorithm:       Algorithm,
		ArtifactSHA256:  hex.EncodeToString(digest[:]),
		PublicKeySHA256: publicFingerprint,
		SignatureBase64: base64.StdEncoding.EncodeToString(sig),
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return Result{}, fmt.Errorf("encode signature: %w", err)
	}
	encoded = append(encoded, '\n')
	f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return Result{}, fmt.Errorf("signature output already exists: %s", outPath)
		}
		return Result{}, fmt.Errorf("create signature: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(outPath)
		}
	}()
	if _, err := f.Write(encoded); err != nil {
		return Result{}, fmt.Errorf("write signature: %w", err)
	}
	if err := f.Close(); err != nil {
		return Result{}, fmt.Errorf("close signature: %w", err)
	}
	ok = true
	return Result{ArtifactSHA256: doc.ArtifactSHA256, PublicKeySHA256: publicFingerprint, SignaturePath: outPath}, nil
}

func VerifyFile(artifactPath, signaturePath, publicKeyPath string) error {
	digest, err := fileDigest(artifactPath)
	if err != nil {
		return err
	}
	doc, err := loadDocument(signaturePath)
	if err != nil {
		return err
	}
	if doc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported signature schema_version %d", doc.SchemaVersion)
	}
	if doc.Algorithm != Algorithm {
		return fmt.Errorf("unsupported signature algorithm %q", doc.Algorithm)
	}
	if doc.ArtifactSHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("signature artifact digest mismatch")
	}
	publicKey, err := loadPublicKey(publicKeyPath)
	if err != nil {
		return err
	}
	fingerprint, err := publicKeyFingerprint(publicKey)
	if err != nil {
		return err
	}
	if doc.PublicKeySHA256 != fingerprint {
		return errors.New("signature is not bound to the trusted public key")
	}
	sig, err := base64.StdEncoding.DecodeString(doc.SignatureBase64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid signature bytes")
	}
	if !ed25519.Verify(publicKey, digest[:], sig) {
		return errors.New("ed25519 signature verification failed")
	}
	return nil
}

func fileDigest(path string) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return zero, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return zero, fmt.Errorf("stat artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return zero, errors.New("artifact must be a regular file")
	}
	if info.Size() > maxArtifactBytes {
		return zero, errors.New("artifact exceeds signature maximum size")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxArtifactBytes+1)); err != nil {
		return zero, fmt.Errorf("hash artifact: %w", err)
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func loadDocument(path string) (Document, error) {
	raw, err := readLimited(path, maxSignatureBytes, "signature")
	if err != nil {
		return Document{}, err
	}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("decode signature: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("signature contains trailing JSON")
		}
		return Document{}, fmt.Errorf("decode trailing signature data: %w", err)
	}
	return doc, nil
}

func loadPrivateKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	raw, err := readLimited(path, maxKeyBytes, "private key")
	if err != nil {
		return nil, nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil, errors.New("private key must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, errors.New("private key is not Ed25519")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return privateKey, publicKey, nil
}

func loadPublicKey(path string) (ed25519.PublicKey, error) {
	raw, err := readLimited(path, maxKeyBytes, "public key")
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("public key must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX public key: %w", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return publicKey, nil
}

func publicKeyFingerprint(publicKey ed25519.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

func readLimited(path string, max int64, label string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", label, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	if info.Size() > max {
		return nil, fmt.Errorf("%s exceeds maximum size", label)
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}
