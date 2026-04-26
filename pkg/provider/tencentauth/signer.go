// Package tencentauth implements the Tencent Cloud API 3.0 TC3-HMAC-SHA256
// request signing shared by the dns/tencent (DNSPod) provider and any future
// Tencent Cloud service providers.
//
// TC3 signing algorithm overview:
//
//  1. Build the canonical request:
//       Method + "\n" + CanonicalURI + "\n" + CanonicalQueryString + "\n" +
//       CanonicalHeaders + "\n" + SignedHeaders + "\n" + hex(sha256(body))
//
//  2. Build the string to sign:
//       "TC3-HMAC-SHA256\n" + timestamp + "\n" + credentialScope + "\n" + hex(sha256(canonicalRequest))
//
//  3. Derive the signing key:
//       secretDate    = HMAC-SHA256("TC3" + SecretKey, date)
//       secretService = HMAC-SHA256(secretDate, service)
//       secretSigning = HMAC-SHA256(secretService, "tc3_request")
//
//  4. Compute the signature:
//       Signature = hex(HMAC-SHA256(secretSigning, stringToSign))
//
//  5. Build the Authorization header:
//       "TC3-HMAC-SHA256 Credential={SecretId}/{credentialScope}, SignedHeaders=content-type;host, Signature={sig}"
//
// Reference: https://cloud.tencent.com/document/api/1427/56166
package tencentauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Signer holds Tencent Cloud API credentials and produces TC3 signed headers.
type Signer struct {
	SecretID  string
	SecretKey string
}

// New creates a new Signer from the given credentials.
func New(secretID, secretKey string) *Signer {
	return &Signer{SecretID: secretID, SecretKey: secretKey}
}

// Headers computes and returns all HTTP headers required for a TC3-signed
// POST request. The returned map contains every header the caller must set;
// no other auth headers are needed.
//
// Parameters:
//
//	host      - Tencent Cloud API host, e.g. "dnspod.tencentcloudapi.com"
//	service   - Service name used in credential scope, e.g. "dnspod"
//	action    - API action name, e.g. "DescribeRecordList"
//	version   - API version string, e.g. "2021-03-23"
//	body      - JSON-encoded request body bytes
//	timestamp - Unix timestamp; use time.Now().Unix() in production
func (s *Signer) Headers(host, service, action, version string, body []byte, timestamp int64) map[string]string {
	const contentType = "application/json"

	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	ts := fmt.Sprintf("%d", timestamp)

	// ── Step 1: Canonical request ─────────────────────────────────────────────
	// All Tencent Cloud 3.0 requests are POST to "/".
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", contentType, host)
	const signedHeaders = "content-type;host"
	hashedPayload := hashHex(body)

	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + hashedPayload

	// ── Step 2: String to sign ────────────────────────────────────────────────
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	stringToSign := "TC3-HMAC-SHA256\n" + ts + "\n" + credentialScope + "\n" + hashHex([]byte(canonicalRequest))

	// ── Step 3: Derive signing key ────────────────────────────────────────────
	secretDate := hmacSHA256([]byte("TC3"+s.SecretKey), []byte(date))
	secretService := hmacSHA256(secretDate, []byte(service))
	secretSigning := hmacSHA256(secretService, []byte("tc3_request"))

	// ── Step 4: Signature ─────────────────────────────────────────────────────
	signature := hex.EncodeToString(hmacSHA256(secretSigning, []byte(stringToSign)))

	// ── Step 5: Authorization header ─────────────────────────────────────────
	authorization := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.SecretID, credentialScope, signedHeaders, signature,
	)

	return map[string]string{
		"Authorization":  authorization,
		"Content-Type":   contentType,
		"Host":           host,
		"X-TC-Action":    action,
		"X-TC-Version":   version,
		"X-TC-Timestamp": ts,
	}
}

// ── Crypto helpers ────────────────────────────────────────────────────────────

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hashHex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
