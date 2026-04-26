package tencentauth

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSecretID  = "AKIDz0imXROFs5ONTJT8df6GWlW3hHG9a9WI"
	testSecretKey = "Gu5t9xGARNpq86cd98joQYCN3Cozk1qA"
	testTimestamp = int64(1599098983) // 2020-09-03
)

func TestHeaders_ContainsAllRequiredFields(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	body := []byte(`{"Domain":"example.com"}`)
	headers := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", body, testTimestamp)

	required := []string{"Authorization", "Content-Type", "Host", "X-TC-Action", "X-TC-Version", "X-TC-Timestamp"}
	for _, key := range required {
		assert.NotEmpty(t, headers[key], "header %q should not be empty", key)
	}

	assert.Equal(t, "application/json", headers["Content-Type"])
	assert.Equal(t, "dnspod.tencentcloudapi.com", headers["Host"])
	assert.Equal(t, "DescribeRecordList", headers["X-TC-Action"])
	assert.Equal(t, "2021-03-23", headers["X-TC-Version"])
	assert.Equal(t, "1599098983", headers["X-TC-Timestamp"])
}

func TestHeaders_AuthorizationFormat(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	headers := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", []byte(`{}`), testTimestamp)

	auth := headers["Authorization"]
	assert.True(t, strings.HasPrefix(auth, "TC3-HMAC-SHA256 "), "authorization should start with TC3-HMAC-SHA256")
	assert.Contains(t, auth, "Credential="+testSecretID+"/")
	assert.Contains(t, auth, "SignedHeaders=content-type;host")
	assert.Contains(t, auth, "Signature=")
}

func TestHeaders_CredentialScopeContainsDate(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	headers := s.Headers("service.tencentcloudapi.com", "myservice", "MyAction", "2021-01-01", []byte(`{}`), testTimestamp)

	// timestamp 1599098983 → date "2020-09-03"
	assert.Contains(t, headers["Authorization"], "2020-09-03/myservice/tc3_request")
}

func TestHeaders_SignatureIsDeterministic(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	body := []byte(`{"Domain":"example.com","Offset":0,"Limit":300}`)

	h1 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", body, testTimestamp)
	h2 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", body, testTimestamp)

	assert.Equal(t, h1["Authorization"], h2["Authorization"], "same inputs must produce same signature")
}

func TestHeaders_DifferentBodiesProduceDifferentSignatures(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	h1 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", []byte(`{"Domain":"a.com"}`), testTimestamp)
	h2 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", []byte(`{"Domain":"b.com"}`), testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_DifferentTimestampsProduceDifferentSignatures(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	body := []byte(`{"Domain":"example.com"}`)
	h1 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "A", "v1", body, 1599098983)
	h2 := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "A", "v1", body, 1599098984)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_DifferentCredentialsProduceDifferentSignatures(t *testing.T) {
	body := []byte(`{"Domain":"example.com"}`)
	h1 := New("ID1", "Secret1").Headers("h.com", "svc", "A", "v1", body, testTimestamp)
	h2 := New("ID2", "Secret2").Headers("h.com", "svc", "A", "v1", body, testTimestamp)

	assert.NotEqual(t, h1["Authorization"], h2["Authorization"])
}

func TestHeaders_SignatureIsValidHex(t *testing.T) {
	s := New(testSecretID, testSecretKey)
	headers := s.Headers("dnspod.tencentcloudapi.com", "dnspod", "DescribeRecordList", "2021-03-23", []byte(`{}`), testTimestamp)

	// Extract Signature= ... from the Authorization header
	auth := headers["Authorization"]
	idx := strings.Index(auth, "Signature=")
	require.True(t, idx >= 0, "Authorization should contain Signature=")
	sig := auth[idx+len("Signature="):]
	// SHA256 HMAC produces 32 bytes → 64 hex chars
	assert.Len(t, sig, 64, "HMAC-SHA256 signature should be 64 hex chars")
	_, err := hex.DecodeString(sig)
	assert.NoError(t, err, "signature should be valid hex")
}

// ── hashHex / hmacSHA256 ──────────────────────────────────────────────────────

func TestHashHex_KnownVector(t *testing.T) {
	// sha256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	got := hashHex([]byte(""))
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", got)
}

func TestHashHex_NonEmpty(t *testing.T) {
	// sha256("abc") verified with openssl dgst -sha256 and sha256sum
	got := hashHex([]byte("abc"))
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
}
