package create

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/libops/sitectl/pkg/config"
)

func TestEnsureJWTKeyPairGeneratesMatchingPEMFiles(t *testing.T) {
	projectDir := t.TempDir()
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}

	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("EnsureJWTKeyPair() error = %v", err)
	}

	privateData := readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename)
	publicData := readJWTKeyTestFile(t, projectDir, jwtPublicKeyFilename)
	privateKey, err := parseJWTPrivateKey(privateData)
	if err != nil {
		t.Fatalf("parseJWTPrivateKey() error = %v", err)
	}
	publicKey, err := parseJWTPublicKey(publicData)
	if err != nil {
		t.Fatalf("parseJWTPublicKey() error = %v", err)
	}
	if !equalRSAPublicKeys(publicKey, &privateKey.PublicKey) {
		t.Fatal("generated JWT public key does not match the private key")
	}

	for _, name := range []string{jwtPrivateKeyFilename, jwtPublicKeyFilename} {
		info, err := os.Stat(filepath.Join(projectDir, "secrets", name))
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", name, got)
		}
	}
}

func TestEnsureJWTKeyPairPreservesValidPair(t *testing.T) {
	projectDir := t.TempDir()
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("first EnsureJWTKeyPair() error = %v", err)
	}
	privateBefore := readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename)
	publicBefore := readJWTKeyTestFile(t, projectDir, jwtPublicKeyFilename)

	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("second EnsureJWTKeyPair() error = %v", err)
	}
	if privateAfter := readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename); !bytes.Equal(privateAfter, privateBefore) {
		t.Fatal("valid JWT private key was rotated")
	}
	if publicAfter := readJWTKeyTestFile(t, projectDir, jwtPublicKeyFilename); !bytes.Equal(publicAfter, publicBefore) {
		t.Fatal("valid JWT public key was rewritten")
	}
}

func TestEnsureJWTKeyPairRepairsLegacyRandomSecrets(t *testing.T) {
	projectDir := t.TempDir()
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	secretsDir := filepath.Join(projectDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, name := range []string{jwtPrivateKeyFilename, jwtPublicKeyFilename} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte("0123456789abcdef"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("EnsureJWTKeyPair() error = %v", err)
	}
	privateKey, err := parseJWTPrivateKey(readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename))
	if err != nil {
		t.Fatalf("parse repaired private key: %v", err)
	}
	publicKey, err := parseJWTPublicKey(readJWTKeyTestFile(t, projectDir, jwtPublicKeyFilename))
	if err != nil {
		t.Fatalf("parse repaired public key: %v", err)
	}
	if !equalRSAPublicKeys(publicKey, &privateKey.PublicKey) {
		t.Fatal("repaired JWT keypair does not match")
	}
}

func TestEnsureJWTKeyPairDerivesPublicKeyWithoutRotatingPrivateKey(t *testing.T) {
	projectDir := t.TempDir()
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("first EnsureJWTKeyPair() error = %v", err)
	}
	privateBefore := readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename)

	otherPrivate, err := rsa.GenerateKey(rand.Reader, jwtRSAKeyBits)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	otherDER, err := x509.MarshalPKIXPublicKey(&otherPrivate.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	publicPath := filepath.Join(projectDir, "secrets", jwtPublicKeyFilename)
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherDER}), 0o600); err != nil {
		t.Fatalf("WriteFile(public key) error = %v", err)
	}

	if err := EnsureJWTKeyPair(ctx); err != nil {
		t.Fatalf("second EnsureJWTKeyPair() error = %v", err)
	}
	if privateAfter := readJWTKeyTestFile(t, projectDir, jwtPrivateKeyFilename); !bytes.Equal(privateAfter, privateBefore) {
		t.Fatal("valid JWT private key was rotated while repairing its public key")
	}
	privateKey, err := parseJWTPrivateKey(privateBefore)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	publicKey, err := parseJWTPublicKey(readJWTKeyTestFile(t, projectDir, jwtPublicKeyFilename))
	if err != nil {
		t.Fatalf("parse repaired public key: %v", err)
	}
	if !equalRSAPublicKeys(publicKey, &privateKey.PublicKey) {
		t.Fatal("repaired JWT public key does not match the preserved private key")
	}
}

func readJWTKeyTestFile(t *testing.T, projectDir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "secrets", name))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}
	return data
}
