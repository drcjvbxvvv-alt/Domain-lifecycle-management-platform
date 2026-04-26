package registrar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ────────────────────────────────────────────────────────────────────

func ncProvider(t *testing.T, handler http.Handler) Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	creds := NamecheapCredentials{
		APIUser:  "testuser",
		APIKey:   "testapikey",
		UserName: "testuser",
		ClientIP: "1.2.3.4",
	}
	return newNamecheapProviderWithClient(creds, srv.URL, srv.Client())
}

// ── XML fixtures ───────────────────────────────────────────────────────────────

const ncListOK = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult>
      <Domain Name="example.com" Created="01/15/2020" Expires="01/15/2025" AutoRenew="true" IsExpired="false"/>
      <Domain Name="ANOTHER.ORG" Created="03/01/2021" Expires="03/01/2026" AutoRenew="false" IsExpired="false"/>
    </DomainGetListResult>
    <Paging>
      <TotalItems>2</TotalItems>
      <CurrentPage>1</CurrentPage>
      <PageSize>100</PageSize>
    </Paging>
  </CommandResponse>
</ApiResponse>`

const ncEmptyList = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult/>
    <Paging>
      <TotalItems>0</TotalItems>
      <CurrentPage>1</CurrentPage>
      <PageSize>100</PageSize>
    </Paging>
  </CommandResponse>
</ApiResponse>`

const ncErrBadKey = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="1011102">API Key is invalid or API access has not been enabled</Error>
  </Errors>
</ApiResponse>`

const ncErrIPNotWhitelisted = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="1011150">No whitelisted IPs found</Error>
  </Errors>
</ApiResponse>`

const ncErrIPNotAllowed = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="1010911">IP 1.2.3.4 is not whitelisted</Error>
  </Errors>
</ApiResponse>`

// ── ncListOK builder for pagination tests ──────────────────────────────────────

func ncPageXML(names []string, total, page, pageSize int) string {
	var domains string
	for _, n := range names {
		domains += `<Domain Name="` + n + `" Created="06/01/2022" Expires="06/01/2027" AutoRenew="true" IsExpired="false"/>`
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult>%s</DomainGetListResult>
    <Paging>
      <TotalItems>%d</TotalItems>
      <CurrentPage>%d</CurrentPage>
      <PageSize>%d</PageSize>
    </Paging>
  </CommandResponse>
</ApiResponse>`, domains, total, page, pageSize)
}

// ── Tests ──────────────────────────────────────────────────────────────────────

func TestNamecheap_ListDomains_HappyPath(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "namecheap.domains.getList", r.URL.Query().Get("Command"))
		assert.Equal(t, "testuser", r.URL.Query().Get("ApiUser"))
		assert.Equal(t, "1.2.3.4", r.URL.Query().Get("ClientIp"))
		w.Write([]byte(ncListOK))
	}))

	domains, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	require.Len(t, domains, 2)

	d0 := domains[0]
	assert.Equal(t, "example.com", d0.FQDN)
	assert.True(t, d0.AutoRenew)
	assert.Equal(t, "ACTIVE", d0.Status)

	require.NotNil(t, d0.RegistrationDate)
	assert.Equal(t, 2020, d0.RegistrationDate.Year())
	assert.Equal(t, time.January, d0.RegistrationDate.Month())
	assert.Equal(t, 15, d0.RegistrationDate.Day())

	require.NotNil(t, d0.ExpiryDate)
	assert.Equal(t, 2025, d0.ExpiryDate.Year())

	// Ensure FQDN is lowercased (ANOTHER.ORG → another.org)
	assert.Equal(t, "another.org", domains[1].FQDN)
	assert.False(t, domains[1].AutoRenew)
}

func TestNamecheap_ListDomains_Empty(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ncEmptyList))
	}))
	domains, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	assert.Empty(t, domains)
}

func TestNamecheap_ListDomains_Pagination(t *testing.T) {
	// 150 domains: page 1 = 100, page 2 = 50
	page1 := make([]string, 100)
	page2 := make([]string, 50)
	for i := 0; i < 100; i++ {
		page1[i] = fmt.Sprintf("domain%03d.com", i)
	}
	for i := 0; i < 50; i++ {
		page2[i] = fmt.Sprintf("domain%03d.com", i+100)
	}

	callCount := 0
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		pageNum := r.URL.Query().Get("Page")
		switch pageNum {
		case "1":
			w.Write([]byte(ncPageXML(page1, 150, 1, 100)))
		case "2":
			w.Write([]byte(ncPageXML(page2, 150, 2, 100)))
		default:
			t.Errorf("unexpected page %q", pageNum)
		}
	}))

	domains, err := p.ListDomains(context.Background())
	require.NoError(t, err)
	assert.Len(t, domains, 150)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "domain000.com", domains[0].FQDN)
	assert.Equal(t, "domain149.com", domains[149].FQDN)
}

func TestNamecheap_ListDomains_InvalidAPIKey(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ncErrBadKey))
	}))
	_, err := p.ListDomains(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized), "expected ErrUnauthorized, got: %v", err)
}

func TestNamecheap_ListDomains_IPNotWhitelisted(t *testing.T) {
	tests := []struct{ name, xml string }{
		{"no whitelisted ips", ncErrIPNotWhitelisted},
		{"ip not allowed", ncErrIPNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tt.xml))
			}))
			_, err := p.ListDomains(context.Background())
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrAccessDenied), "expected ErrAccessDenied, got: %v", err)
		})
	}
}

func TestNamecheap_ListDomains_UnknownAPIError(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors><Error Number="9999">Some unknown error</Error></Errors>
</ApiResponse>`
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(xml))
	}))
	_, err := p.ListDomains(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "9999")
	assert.Contains(t, err.Error(), "Some unknown error")
}

func TestNamecheap_GetDomain_Found(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ncListOK))
	}))
	info, err := p.GetDomain(context.Background(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "example.com", info.FQDN)
}

func TestNamecheap_GetDomain_NotFound(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ncListOK))
	}))
	_, err := p.GetDomain(context.Background(), "notexist.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDomainNotFound))
}

func TestNamecheap_GetDomain_CaseInsensitive(t *testing.T) {
	p := ncProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ncListOK))
	}))
	// "ANOTHER.ORG" stored in XML but queried lowercase
	info, err := p.GetDomain(context.Background(), "ANOTHER.ORG")
	require.NoError(t, err)
	assert.Equal(t, "another.org", info.FQDN)
}

func TestNamecheap_NewProvider_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty json", `{}`},
		{"missing api_key", `{"api_user":"u","client_ip":"1.1.1.1"}`},
		{"missing client_ip", `{"api_user":"u","api_key":"k"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newNamecheapProvider([]byte(tt.creds))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMissingCredentials))
		})
	}
}

func TestNamecheap_DefaultUsername(t *testing.T) {
	// If username is omitted, it should default to api_user
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// UserName in request should equal ApiUser
		assert.Equal(t, r.URL.Query().Get("ApiUser"), r.URL.Query().Get("UserName"))
		w.Write([]byte(ncEmptyList))
	}))
	defer srv.Close()

	creds := NamecheapCredentials{
		APIUser:  "myuser",
		APIKey:   "mykey",
		UserName: "", // omitted — should default to APIUser
		ClientIP: "1.2.3.4",
	}
	p := newNamecheapProviderWithClient(creds, srv.URL, srv.Client())
	_, err := p.ListDomains(context.Background())
	require.NoError(t, err)
}
