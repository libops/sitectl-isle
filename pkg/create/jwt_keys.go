package create

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
)

const (
	jwtPrivateKeyFilename = "JWT_PRIVATE_KEY"
	jwtPublicKeyFilename  = "JWT_PUBLIC_KEY"
	jwtRSAKeyBits         = 2048
)

// EnsureJWTKeyPair makes sure the selected ISLE checkout has a usable JWT RSA
// keypair before the Compose init service creates the remaining site secrets.
// Valid private keys are preserved so rerunning create never rotates a site's
// signing identity. Missing, malformed, or mismatched public keys are derived
// from the existing private key.
func EnsureJWTKeyPair(ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if projectDir == "" {
		return fmt.Errorf("project directory is empty")
	}

	secretsDir := filepath.Join(projectDir, "secrets")
	privatePath := filepath.Join(secretsDir, jwtPrivateKeyFilename)
	publicPath := filepath.Join(secretsDir, jwtPublicKeyFilename)

	privateKey, err := readJWTPrivateKey(ctx, privatePath)
	if err != nil {
		return err
	}
	if privateKey == nil {
		privateKey, err = rsa.GenerateKey(rand.Reader, jwtRSAKeyBits)
		if err != nil {
			return fmt.Errorf("generate Islandora JWT private key: %w", err)
		}
		privateData := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		})
		if err := ctx.WriteFile(privatePath, privateData); err != nil {
			return fmt.Errorf("write Islandora JWT private key: %w", err)
		}
	}

	publicData, err := marshalJWTPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}
	publicKey, err := readJWTPublicKey(ctx, publicPath)
	if err != nil {
		return err
	}
	if publicKey == nil || !equalRSAPublicKeys(publicKey, &privateKey.PublicKey) {
		if err := ctx.WriteFile(publicPath, publicData); err != nil {
			return fmt.Errorf("write Islandora JWT public key: %w", err)
		}
	}
	return nil
}

func readJWTPrivateKey(ctx *config.Context, path string) (*rsa.PrivateKey, error) {
	data, err := ctx.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Islandora JWT private key: %w", err)
	}
	key, err := parseJWTPrivateKey(data)
	if err != nil {
		return nil, nil
	}
	return key, nil
}

func parseJWTPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid PEM data")
	}

	var key *rsa.PrivateKey
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			var ok bool
			key, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("private key is not RSA")
			}
		}
	default:
		return nil, fmt.Errorf("unsupported private key PEM type %q", block.Type)
	}
	if err != nil {
		return nil, err
	}
	if key.N.BitLen() < jwtRSAKeyBits {
		return nil, fmt.Errorf("RSA private key is smaller than %d bits", jwtRSAKeyBits)
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return key, nil
}

func readJWTPublicKey(ctx *config.Context, path string) (*rsa.PublicKey, error) {
	data, err := ctx.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Islandora JWT public key: %w", err)
	}
	key, err := parseJWTPublicKey(data)
	if err != nil {
		return nil, nil
	}
	return key, nil
}

func parseJWTPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid PEM data")
	}

	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not RSA")
		}
		return key, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported public key PEM type %q", block.Type)
	}
}

func marshalJWTPublicKey(key *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal Islandora JWT public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func equalRSAPublicKeys(left, right *rsa.PublicKey) bool {
	return left != nil && right != nil && left.E == right.E && left.N.Cmp(right.N) == 0
}
