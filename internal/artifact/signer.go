package artifact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Signer produces and verifies artifact signatures.
// Phase 1: HMAC-SHA256 with a shared secret.
// Future (ADR-0004): cosign / GPG / ECDSA.
type Signer interface {
	Sign(checksum string) (string, error)
	Verify(checksum, signature string) error
}

// HMACSigner implements Signer using HMAC-SHA256.
type HMACSigner struct {
	secret []byte
}

// NewHMACSigner creates a signer with the given secret key.
func NewHMACSigner(secret string) *HMACSigner {
	return &HMACSigner{secret: []byte(secret)}
}

func (s *HMACSigner) Sign(checksum string) (string, error) {
	if checksum == "" {
		return "", fmt.Errorf("signer: checksum is empty")
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(checksum))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *HMACSigner) Verify(checksum, signature string) error {
	expected, err := s.Sign(checksum)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return ErrSignatureInvalid
	}
	return nil
}
