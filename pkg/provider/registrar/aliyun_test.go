package registrar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── test helpers ───────────────────────────────────────────────────────────────

func aliyunTestProvider(t *testing.T, handler http.Handler) Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	creds := AliyunCredentials{
		AccessKeyID:     "test-key-id",
		AccessKeySecret: "test-key-secret",
	}
	return newAliyunProviderWithClient(creds, srv.URL, srv.Client())
}

// buildListResponse constructs a QueryDomainList response body.
func buildListResponse(domains []aliyunDomainItem, total, page int, hasNext bool) []byte {
	resp := aliyunDomainListResponse{
		RequestId: "test-request-id",
		Data: aliyunDomainListData{
			Domain:         domains,
			TotalItemNum:   total,
			CurrentPageNum: page,
			NextPage:       hasNext,
			PageSize:       aliyunPageSize,
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

// buildSingleDomainResponse constructs a QueryDomainByDomainName response.
func buildSingleDomainResponse(d aliyunDomainItem) []byte {
	resp := aliyunSingleDomainData{
		DomainName:       d.DomainName,
		RegistrationDate: d.RegistrationDate,
		ExpirationDate:   d.ExpirationDate,
		AutoRenew:        d.AutoRenew,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

func aliyunErrorBody(code, message string) []byte {
	b, _ := json.Marshal(aliyunErrorResponse{Code: code, Message: message, RequestId: "req-id"})
	return b
}

// ── newAliyunProvider (factory) ────────────────────────────────────────────────

func TestNewAliyunProvider_OK(t *testing.T) {
	creds, _ := json.Marshal(AliyunCredentials{AccessKeyID: "kid", AccessKeySecret: "ksecret"})
	p, err := newAliyunProvider(creds)
	require.NoError(t, err)
	assert.Equal(t, "aliyun", p.Name())
}

func TestNewAliyunProvider_MissingAccessKeyID(t *testing.T) {
	creds, _ := json.Marshal(AliyunCredentials{AccessKeySecret: "ksecret"})
	_, err := newAliyunProvider(creds)
	assert.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewAliyunProvider_MissingAccessKeySecret(t *testing.T) {
	creds, _ := json.Marshal(AliyunCredentials{AccessKeyID: "kid"})
	_, err := newAliyunProvider(creds)
	assert.ErrorIs(t, err, ErrMissingCredentials)
}

func TestNewAliyunProvider_InvalidJSON(t *testing.T) {
	_, err := newAliyunProvider(json.RawMessage(`{bad json}`))
	assert.ErrorIs(t, err, ErrMissingCredentials)
}

// ── aliyunEncode ───────────────────────────────────────────────────────────────

func TestAliyunEncode_UnreservedPassthrough(t *testing.T) {
	// RFC3986 unreserved chars must not be encoded
	input := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	assert.Equal(t, input, aliyunEncode(input))
}

func TestAliyunEncode_SpecialChars(t *testing.T) {
	assert.Equal(t, "%2F", aliyunEncode("/"))
	assert.Equal(t, "%3D", aliyunEncode("="))
	assert.Equal(t, "%26", aliyunEncode("&"))
	assert.Equal(t, "%2B", aliyunEncode("+"))
	assert.Equal(t, "%20", aliyunEncode(" "))
}

// ── ListDomains — happy path ───────────────────────────────────────────────────

func TestAliyunListDomains_SinglePage(t *testing.T) {
	reg := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	exp := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	items := []aliyunDomainItem{
		{
			DomainName:       "example.com",
			RegistrationDate: reg.Format(aliyunDateFormat),
			ExpirationDate:   exp.Format(aliyunDateFormat),
			AutoRenew:        true,
		},
		{
			DomainName:       "HELLO.org",
			RegistrationDate: reg.Format(aliyunDateFormat),
			ExpirationDate:   exp.Format(aliyunDateFormat),
			AutoRenew:        false,
		},
		{
			DomainName:       "  test.net  ",
			RegistrationDate: reg.Format(aliyunDateFormat),
			ExpirationDate:   exp.Format(aliyunDateFormat),
			AutoRenew:        true,
		},
	}

	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "QueryDomainList", r.URL.Query().Get("Action"))
		assert.Equal(t, "1", r.URL.Query().Get("PageNum"))
		assert.Equal(t, "100", r.URL.Query().Get("PageSize"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(buildListResponse(items, 3, 1, false))
	}))

	result, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Verify FQDN normalisation
	assert.Equal(t, "example.com", result[0].FQDN)
	assert.Equal(t, "hello.org", result[1].FQDN)  // lowercased
	assert.Equal(t, "test.net", result[2].FQDN)    // trimmed + lowercased

	// Verify AutoRenew
	assert.True(t, result[0].AutoRenew)
	assert.False(t, result[1].AutoRenew)
	assert.True(t, result[2].AutoRenew)

	// Verify date parsing
	require.NotNil(t, result[0].RegistrationDate)
	require.NotNil(t, result[0].ExpiryDate)
	assert.Equal(t, reg.Unix(), result[0].RegistrationDate.Unix())
	assert.Equal(t, exp.Unix(), result[0].ExpiryDate.Unix())
}

// ── ListDomains — pagination (150 domains across 2 pages) ─────────────────────

func TestAliyunListDomains_Pagination(t *testing.T) {
	calls := 0
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")

		page := r.URL.Query().Get("PageNum")
		switch page {
		case "1":
			// First page: 100 items, NextPage=true
			batch := make([]aliyunDomainItem, 100)
			for i := range batch {
				batch[i] = aliyunDomainItem{DomainName: fmt.Sprintf("domain%04d.com", i)}
			}
			w.Write(buildListResponse(batch, 150, 1, true))
		case "2":
			// Second page: 50 items, NextPage=false
			batch := make([]aliyunDomainItem, 50)
			for i := range batch {
				batch[i] = aliyunDomainItem{DomainName: fmt.Sprintf("domain%04d.com", 100+i)}
			}
			w.Write(buildListResponse(batch, 150, 2, false))
		default:
			t.Errorf("unexpected page: %s", page)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	result, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 150)
	assert.Equal(t, 2, calls)

	// Spot-check first and last
	assert.Equal(t, "domain0000.com", result[0].FQDN)
	assert.Equal(t, "domain0149.com", result[149].FQDN)
}

// ── ListDomains — empty account ────────────────────────────────────────────────

func TestAliyunListDomains_Empty(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(buildListResponse(nil, 0, 1, false))
	}))

	result, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result)
}

// ── ListDomains — error codes ──────────────────────────────────────────────────

func TestAliyunListDomains_InvalidAccessKeyNotFound(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("InvalidAccessKeyId.NotFound", "Specified access key is not found."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAliyunListDomains_InvalidAccessKey(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("InvalidAccessKeyId", "The specified AccessKeyId does not exist."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAliyunListDomains_SignatureDoesNotMatch(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("SignatureDoesNotMatch", "The request signature does not match."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAliyunListDomains_ForbiddenRAM(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("Forbidden.RAM", "User not authorized."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrAccessDenied)
}

func TestAliyunListDomains_Throttling(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("Throttling", "Request was denied due to user flow control."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
}

func TestAliyunListDomains_ServiceUnavailableTemporary(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("ServiceUnavailableTemporary", "Service temporarily unavailable."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
}

func TestAliyunListDomains_HTTP401(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(aliyunErrorBody("InvalidAccessKeyId", "Bad key."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAliyunListDomains_HTTP403ForbiddenRAM(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write(aliyunErrorBody("Forbidden.RAM", "No permission."))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrAccessDenied)
}

func TestAliyunListDomains_HTTP429(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
}

func TestAliyunListDomains_ServerError(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"Code":"InternalError","Message":"internal"}`))
	}))

	_, err := p.ListDomains(context.Background())
	assert.ErrorContains(t, err, "500")
}

// ── GetDomain — happy path ─────────────────────────────────────────────────────

func TestAliyunGetDomain_Found(t *testing.T) {
	reg := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	exp := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	item := aliyunDomainItem{
		DomainName:       "example.com",
		RegistrationDate: reg.Format(aliyunDateFormat),
		ExpirationDate:   exp.Format(aliyunDateFormat),
		AutoRenew:        true,
	}

	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "QueryDomainByDomainName", r.URL.Query().Get("Action"))
		assert.Equal(t, "example.com", r.URL.Query().Get("DomainName"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(buildSingleDomainResponse(item))
	}))

	info, err := p.GetDomain(context.Background(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "example.com", info.FQDN)
	assert.True(t, info.AutoRenew)
	require.NotNil(t, info.RegistrationDate)
	require.NotNil(t, info.ExpiryDate)
	assert.Equal(t, reg.Unix(), info.RegistrationDate.Unix())
	assert.Equal(t, exp.Unix(), info.ExpiryDate.Unix())
}

func TestAliyunGetDomain_UppercaseFQDNNormalized(t *testing.T) {
	item := aliyunDomainItem{DomainName: "EXAMPLE.COM"}

	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The request param should be lowercased
		assert.Equal(t, "example.com", r.URL.Query().Get("DomainName"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(buildSingleDomainResponse(item))
	}))

	info, err := p.GetDomain(context.Background(), "EXAMPLE.COM")
	require.NoError(t, err)
	assert.Equal(t, "example.com", info.FQDN)
}

// ── GetDomain — not found ──────────────────────────────────────────────────────

func TestAliyunGetDomain_EmptyDomainName(t *testing.T) {
	// Aliyun returns an empty DomainName when the domain is not found in the account.
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Empty DomainName in response
		w.Write([]byte(`{"DomainName":"","RegistrationDate":"","ExpirationDate":"","AutoRenew":false}`))
	}))

	_, err := p.GetDomain(context.Background(), "notexist.com")
	assert.ErrorIs(t, err, ErrDomainNotFound)
}

func TestAliyunGetDomain_HTTP404(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := p.GetDomain(context.Background(), "notexist.com")
	assert.ErrorIs(t, err, ErrDomainNotFound)
}

func TestAliyunGetDomain_InvalidAccessKey(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("InvalidAccessKeyId.NotFound", "Specified access key is not found."))
	}))

	_, err := p.GetDomain(context.Background(), "example.com")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestAliyunGetDomain_ForbiddenRAM(t *testing.T) {
	p := aliyunTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(aliyunErrorBody("Forbidden.RAM", "User not authorized."))
	}))

	_, err := p.GetDomain(context.Background(), "example.com")
	assert.ErrorIs(t, err, ErrAccessDenied)
}

// ── Registry ───────────────────────────────────────────────────────────────────

func TestRegistryGet_Aliyun(t *testing.T) {
	creds, _ := json.Marshal(AliyunCredentials{AccessKeyID: "kid", AccessKeySecret: "ksecret"})
	p, err := Get("aliyun", creds)
	require.NoError(t, err)
	assert.Equal(t, "aliyun", p.Name())
}

func TestRegisteredTypes_ContainsAliyun(t *testing.T) {
	types := RegisteredTypes()
	assert.Contains(t, types, "aliyun")
}

// ── aliyunToDomainInfo ─────────────────────────────────────────────────────────

func TestAliyunToDomainInfo_MissingDates(t *testing.T) {
	d := aliyunDomainItem{DomainName: "example.com"}
	info := aliyunToDomainInfo(d)
	assert.Nil(t, info.RegistrationDate)
	assert.Nil(t, info.ExpiryDate)
}

func TestAliyunToDomainInfo_InvalidDateFormat(t *testing.T) {
	d := aliyunDomainItem{
		DomainName:       "example.com",
		RegistrationDate: "not-a-date",
		ExpirationDate:   "also-not-a-date",
	}
	info := aliyunToDomainInfo(d)
	assert.Nil(t, info.RegistrationDate)
	assert.Nil(t, info.ExpiryDate)
}
