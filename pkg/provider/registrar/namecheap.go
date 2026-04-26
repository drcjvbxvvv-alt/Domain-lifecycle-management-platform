package registrar

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	Register("namecheap", newNamecheapProvider)
}

// ── Credentials ────────────────────────────────────────────────────────────────

// NamecheapCredentials is the expected shape of registrar_accounts.credentials
// for Namecheap accounts.
//
// IMPORTANT: client_ip must be the IP address of the server running this
// application, and it must be whitelisted in the Namecheap account under
// Profile > Tools > Namecheap API Access > Whitelisted IPs.
//
// JSON example:
//
//	{
//	  "api_user": "myusername",
//	  "api_key": "xxxxxxxxxxxx",
//	  "username": "myusername",
//	  "client_ip": "1.2.3.4",
//	  "environment": "production"
//	}
type NamecheapCredentials struct {
	APIUser     string `json:"api_user"`
	APIKey      string `json:"api_key"`
	UserName    string `json:"username"`    // usually same as APIUser; defaults to APIUser if omitted
	ClientIP    string `json:"client_ip"`   // must be whitelisted in Namecheap account
	Environment string `json:"environment"` // "production" (default) or "sandbox"
}

// ── Provider ───────────────────────────────────────────────────────────────────

type namecheapProvider struct {
	creds   NamecheapCredentials
	baseURL string
	client  *http.Client
}

func newNamecheapProvider(credentials json.RawMessage) (Provider, error) {
	var creds NamecheapCredentials
	if err := json.Unmarshal(credentials, &creds); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingCredentials, err)
	}
	if strings.TrimSpace(creds.APIUser) == "" || strings.TrimSpace(creds.APIKey) == "" {
		return nil, fmt.Errorf("%w: api_user and api_key are required", ErrMissingCredentials)
	}
	if strings.TrimSpace(creds.ClientIP) == "" {
		return nil, fmt.Errorf("%w: client_ip is required — add this server's IP to Namecheap Profile > Tools > API Access", ErrMissingCredentials)
	}
	if strings.TrimSpace(creds.UserName) == "" {
		creds.UserName = creds.APIUser
	}

	baseURL := "https://api.namecheap.com/xml.response"
	if creds.Environment == "sandbox" {
		baseURL = "https://api.sandbox.namecheap.com/xml.response"
	}

	return &namecheapProvider{
		creds:   creds,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// newNamecheapProviderWithClient allows injecting a custom HTTP client (for tests).
func newNamecheapProviderWithClient(creds NamecheapCredentials, baseURL string, client *http.Client) Provider {
	if strings.TrimSpace(creds.UserName) == "" {
		creds.UserName = creds.APIUser
	}
	return &namecheapProvider{creds: creds, baseURL: baseURL, client: client}
}

func (p *namecheapProvider) Name() string { return "namecheap" }

// ── Wire types (Namecheap XML response shapes) ─────────────────────────────────

type ncAPIResponse struct {
	XMLName         xml.Name          `xml:"ApiResponse"`
	Status          string            `xml:"Status,attr"`
	Errors          []ncAPIError      `xml:"Errors>Error"`
	CommandResponse *ncCommandResponse `xml:"CommandResponse"`
}

type ncAPIError struct {
	Number string `xml:"Number,attr"`
	Text   string `xml:",chardata"`
}

type ncCommandResponse struct {
	Domains []ncDomain `xml:"DomainGetListResult>Domain"`
	Paging  ncPaging   `xml:"Paging"`
}

type ncDomain struct {
	Name      string `xml:"Name,attr"`
	Created   string `xml:"Created,attr"`
	Expires   string `xml:"Expires,attr"`
	AutoRenew string `xml:"AutoRenew,attr"` // "true" / "false"
	IsExpired string `xml:"IsExpired,attr"`
}

type ncPaging struct {
	TotalItems  int `xml:"TotalItems"`
	CurrentPage int `xml:"CurrentPage"`
	PageSize    int `xml:"PageSize"`
}

const ncPageSize = 100

// ── ListDomains ────────────────────────────────────────────────────────────────

func (p *namecheapProvider) ListDomains(ctx context.Context) ([]DomainInfo, error) {
	var all []DomainInfo
	page := 1

	for {
		domains, paging, err := p.fetchPage(ctx, page)
		if err != nil {
			return nil, err
		}
		all = append(all, domains...)

		// Calculate total pages from paging data.
		if paging.TotalItems == 0 {
			break
		}
		totalPages := (paging.TotalItems + ncPageSize - 1) / ncPageSize
		if page >= totalPages {
			break
		}
		page++
	}

	return all, nil
}

func (p *namecheapProvider) fetchPage(ctx context.Context, page int) ([]DomainInfo, ncPaging, error) {
	params := url.Values{
		"ApiUser":  {p.creds.APIUser},
		"ApiKey":   {p.creds.APIKey},
		"UserName": {p.creds.UserName},
		"ClientIp": {p.creds.ClientIP},
		"Command":  {"namecheap.domains.getList"},
		"PageSize": {fmt.Sprintf("%d", ncPageSize)},
		"Page":     {fmt.Sprintf("%d", page)},
	}

	reqURL := p.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, ncPaging{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, ncPaging{}, fmt.Errorf("namecheap list domains: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ncPaging{}, fmt.Errorf("namecheap HTTP %d", resp.StatusCode)
	}

	var apiResp ncAPIResponse
	if err := xml.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, ncPaging{}, fmt.Errorf("parse namecheap response: %w", err)
	}

	if apiResp.Status == "ERROR" {
		return nil, ncPaging{}, mapNamecheapErrors(apiResp.Errors)
	}

	if apiResp.CommandResponse == nil {
		return nil, ncPaging{}, fmt.Errorf("namecheap: empty command response")
	}

	var domains []DomainInfo
	for _, d := range apiResp.CommandResponse.Domains {
		domains = append(domains, ncToDomainInfo(d))
	}
	return domains, apiResp.CommandResponse.Paging, nil
}

// ── GetDomain ──────────────────────────────────────────────────────────────────

// GetDomain finds a single domain by scanning the full list.
// Namecheap's basic API does not expose a per-domain detail endpoint
// that returns registration/expiry dates directly.
func (p *namecheapProvider) GetDomain(ctx context.Context, fqdn string) (*DomainInfo, error) {
	all, err := p.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(strings.TrimSpace(fqdn))
	for i := range all {
		if all[i].FQDN == target {
			return &all[i], nil
		}
	}
	return nil, ErrDomainNotFound
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// ncToDomainInfo converts a Namecheap XML domain item to DomainInfo.
func ncToDomainInfo(d ncDomain) DomainInfo {
	info := DomainInfo{
		FQDN:      strings.ToLower(strings.TrimSpace(d.Name)),
		AutoRenew: strings.EqualFold(d.AutoRenew, "true"),
		Status:    "ACTIVE",
	}
	if strings.EqualFold(d.IsExpired, "true") {
		info.Status = "EXPIRED"
	}

	// Namecheap date format: MM/DD/YYYY
	const layout = "01/02/2006"
	if t, err := time.Parse(layout, d.Created); err == nil {
		info.RegistrationDate = &t
	}
	if t, err := time.Parse(layout, d.Expires); err == nil {
		info.ExpiryDate = &t
	}
	return info
}

// mapNamecheapErrors maps Namecheap XML error codes to typed sentinels.
//
// Error codes reference: https://www.namecheap.com/support/api/error-codes/
func mapNamecheapErrors(errs []ncAPIError) error {
	if len(errs) == 0 {
		return fmt.Errorf("namecheap API error (no details)")
	}
	for _, e := range errs {
		text := strings.TrimSpace(e.Text)
		switch e.Number {
		case "1011102": // Invalid API key / API access not enabled
			return fmt.Errorf("%w: %s", ErrUnauthorized, text)
		case "1011150", "1010911": // No whitelisted IPs / IP not in whitelist
			return fmt.Errorf("%w: IP not in whitelist — add this server's IP in Namecheap Profile > Tools > API Access. %s", ErrAccessDenied, text)
		}
	}
	// Return first error as generic
	return fmt.Errorf("namecheap API error %s: %s", errs[0].Number, strings.TrimSpace(errs[0].Text))
}
