// Package huaweiauth implements the Huawei Cloud API AK/SK (SDK-HMAC-SHA256)
// request signing shared by the dns/huawei provider and any future Huawei
// Cloud service providers.
//
// SDK-HMAC-SHA256 signing algorithm overview:
//
//  1. Build the canonical request:
//       Method + "\n" + CanonicalURI + "\n" + CanonicalQueryString + "\n" +
//       CanonicalHeaders + "\n" + SignedHeaders + "\n" + hex(sha256(body))
//
//  2. Build the string to sign:
//       "SDK-HMAC-SHA256\n" + datetimeUTC + "\n" + hex(sha256(canonicalRequest))
//
//  3. Compute the signature:
//       Signature = hex(HMAC-SHA256(secretKey, stringToSign))
//
//  4. Build the Authorization header:
//       "SDK-HMAC-SHA256 Access={AccessKey}, SignedHeaders={signedHeaders}, Signature={sig}"
//
// Key differences from TC3-HMAC-SHA256:
//   - No date-based key derivation chain; the raw SecretKey is used directly.
//   - Date/time lives in the "X-Sdk-Date" header (format "20060102T150405Z").
//   - String-to-sign uses the raw datetime, not a credential scope.
//   - SignedHeaders always include content-type, host, x-sdk-date (sorted).
//
// Reference: https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_68001.html
package huaweiauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Signer holds Huawei Cloud API credentials and produces SDK-HMAC-SHA256
// signed headers for HTTP requests.
type Signer struct {
	AccessKey string
	SecretKey string
}

// New creates a new Signer from the given AK/SK credentials.
func New(accessKey, secretKey string) *Signer {
	return &Signer{AccessKey: accessKey, SecretKey: secretKey}
}

// Headers computes and returns all HTTP headers required for an AK/SK-signed
// request. The returned map contains every header the caller must set.
//
// Parameters:
//
//	method      - HTTP method, e.g. "GET", "POST"
//	rawURL      - Full request URL including scheme, host, path, and query
//	body        - Request body bytes (nil or empty for GET/DELETE)
//	contentType - Content-Type header value, e.g. "application/json"
//	timestamp   - Unix timestamp; use time.Now().Unix() in production
func (s *Signer) Headers(method, rawURL string, body []byte, contentType string, timestamp int64) (map[string]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("huaweiauth: parse URL: %w", err)
	}

	host := u.Host
	datetime := time.Unix(timestamp, 0).UTC().Format("20060102T150405Z")

	// ── Step 1: Canonical request ─────────────────────────────────────────────

	// Canonical URI: percent-encoded path
	canonicalURI := u.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Canonical query string: sorted key=value pairs, both URL-encoded
	canonicalQueryString := canonicalQuery(u.Query())

	// Canonical headers and signed headers
	// Huawei requires content-type, host, x-sdk-date (sorted alphabetically)
	signedHeaders := "content-type;host;x-sdk-date"
	canonicalHeaders := fmt.Sprintf(
		"content-type:%s\nhost:%s\nx-sdk-date:%s\n",
		contentType, host, datetime,
	)

	hashedPayload := hashHex(body)

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")

	// ── Step 2: String to sign ────────────────────────────────────────────────
	stringToSign := "SDK-HMAC-SHA256\n" + datetime + "\n" + hashHex([]byte(canonicalRequest))

	// ── Step 3: Signature ─────────────────────────────────────────────────────
	signature := hex.EncodeToString(hmacSHA256([]byte(s.SecretKey), []byte(stringToSign)))

	// ── Step 4: Authorization header ─────────────────────────────────────────
	authorization := fmt.Sprintf(
		"SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		s.AccessKey, signedHeaders, signature,
	)

	return map[string]string{
		"Authorization": authorization,
		"Content-Type":  contentType,
		"Host":          host,
		"X-Sdk-Date":    datetime,
	}, nil
}

// canonicalQuery returns a URL-encoded, sorted canonical query string from the
// given query values, as required by the SDK-HMAC-SHA256 algorithm.
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}

	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
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
