package huaweiauth

import (
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccessKey = "UDSIAMSTUBTEST000094"
	testSecretKey = "Wuv0T3HLfvVQHsuzRrGSrKvNLJzQVEwDlFBSHcaX"
	testTimestamp = int64(1677643200) // 2023-03-01T00:00:00Z
)

func TestHeaders_ContainsAllRequiredFields(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	headers, err := s.Headers("GET", "https://dns.myhuaweicloud.com/v2/zones", nil, "application/json", testTimestamp)
	require.NoError(t, err)

	required := []string{"Authorization", "Content-Type", "Host", "X-Sdk-Date"}
	for _, key := range required {
		assert.NotEmpty(t, headers[key], "header %q should not be empty", key)
	}

	assert.Equal(t, "application/json", headers["Content-Type"])
	assert.Equal(t, "dns.myhuaweicloud.com", headers["Host"])
	assert.Equal(t, "20230301T040000Z", headers["X-Sdk-Date"])
}

func TestHeaders_AuthorizationFormat(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	headers, err := s.Headers("POST", "https://dns.myhuaweicloud.com/v2/zones/abc/recordsets", []byte(`{}`), "application/json", testTimestamp)
	require.NoError(t, err)

	auth := headers["Authorization"]
	assert.True(t, strings.HasPrefix(auth, "SDK-HMAC-SHA256 "), "should start with SDK-HMAC-SHA256")
	assert.Contains(t, auth, "Access="+testAccessKey)
	assert.Contains(t, auth, "SignedHeaders=content-type;host;x-sdk-date")
	assert.Contains(t, auth, "Signature=")
}

func TestHeaders_SignatureIsDeterministic(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	body := []byte(`{"name":"www.example.com.","type":"A","records":["1.2.3.4"],"ttl":300}`)
	reqURL := "https://dns.myhuaweicloud.com/v2/zones/zone-abc/recordsets"

	h1, err1 := s.Headers("POST", reqURL, body, "application/json", testTimestamp)
	h2, err2 := s.Headers("POST", reqURL, body, "application/json", testTimestamp)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, h1["Authorization"], h2["Authorization"], "same inputs → same signature")
}

func TestHeaders_DifferentBodiesProduceDifferentSignatures(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	reqURL := "https://dns.myhuaweicloud.com/v2/zones/zone-abc/recordsets"

	h1, _ := s.Headers("POST", reqURL, []byte(`{"records":["1.1.1.1"]}`), "application/json", testTimestamp)
	h2, _ := s.Headers("POST", reqURL, []byte(`{"records":["2.2.2.2"]}`), "application/json", testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_DifferentTimestampsProduceDifferentSignatures(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	reqURL := "https://dns.myhuaweicloud.com/v2/zones"

	h1, _ := s.Headers("GET", reqURL, nil, "application/json", testTimestamp)
	h2, _ := s.Headers("GET", reqURL, nil, "application/json", testTimestamp+1)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_DifferentCredentialsProduceDifferentSignatures(t *testing.T) {
	reqURL := "https://dns.myhuaweicloud.com/v2/zones"

	h1, _ := New("AK1", "SK1").Headers("GET", reqURL, nil, "application/json", testTimestamp)
	h2, _ := New("AK2", "SK2").Headers("GET", reqURL, nil, "application/json", testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_DifferentMethodsProduceDifferentSignatures(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	reqURL := "https://dns.myhuaweicloud.com/v2/zones/id/recordsets/rid"

	h1, _ := s.Headers("PUT", reqURL, []byte(`{}`), "application/json", testTimestamp)
	h2, _ := s.Headers("DELETE", reqURL, nil, "application/json", testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_SignatureIsValidHex(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	headers, err := s.Headers("GET", "https://dns.myhuaweicloud.com/v2/zones", nil, "application/json", testTimestamp)
	require.NoError(t, err)

	auth := headers["Authorization"]
	idx := strings.Index(auth, "Signature=")
	require.True(t, idx >= 0)
	sig := auth[idx+len("Signature="):]

	assert.Len(t, sig, 64, "HMAC-SHA256 → 32 bytes → 64 hex chars")
	_, err = hex.DecodeString(sig)
	assert.NoError(t, err, "signature must be valid hex")
}

func TestHeaders_QueryStringIsIncludedInSignature(t *testing.T) {
	s := New(testAccessKey, testSecretKey)

	h1, _ := s.Headers("GET", "https://dns.myhuaweicloud.com/v2/zones?name=a.com", nil, "application/json", testTimestamp)
	h2, _ := s.Headers("GET", "https://dns.myhuaweicloud.com/v2/zones?name=b.com", nil, "application/json", testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_XSdkDateFormat(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	// timestamp 1599098983 → 2020-09-03T02:09:43Z → "20200903T020943Z"
	headers, err := s.Headers("GET", "https://dns.myhuaweicloud.com/v2/zones", nil, "application/json", 1599098983)
	require.NoError(t, err)

	assert.Equal(t, "20200903T020943Z", headers["X-Sdk-Date"])
}

func TestHeaders_InvalidURL(t *testing.T) {
	s := New(testAccessKey, testSecretKey)
	_, err := s.Headers("GET", "://bad-url", nil, "application/json", testTimestamp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse URL")
}

// ── canonicalQuery ────────────────────────────────────────────────────────────

func TestCanonicalQuery_Empty(t *testing.T) {
	assert.Equal(t, "", canonicalQuery(nil))
}

func TestCanonicalQuery_Sorted(t *testing.T) {
	q := url.Values{"zzz": {"3"}, "aaa": {"1"}, "mmm": {"2"}}
	result := canonicalQuery(q)
	assert.Equal(t, "aaa=1&mmm=2&zzz=3", result)
}

func TestCanonicalQuery_SpecialCharsEncoded(t *testing.T) {
	q := url.Values{"name": {"my domain.com"}}
	result := canonicalQuery(q)
	// url.QueryEscape encodes space as +
	assert.Contains(t, result, "name=")
	assert.NotContains(t, result, " ") // space must be encoded
}

func TestCanonicalQuery_MultipleValuesForSameKey(t *testing.T) {
	q := url.Values{"type": {"A", "AAAA"}}
	result := canonicalQuery(q)
	// Both values should appear, sorted
	assert.Contains(t, result, "type=A")
	assert.Contains(t, result, "type=AAAA")
}

// ── hashHex ───────────────────────────────────────────────────────────────────

func TestHashHex_EmptyString(t *testing.T) {
	got := hashHex([]byte(""))
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", got)
}

func TestHashHex_ABC(t *testing.T) {
	got := hashHex([]byte("abc"))
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
}
