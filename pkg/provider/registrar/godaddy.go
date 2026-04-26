package registrar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("godaddy", newGoDaddyProvider)
}

// ── Credentials ────────────────────────────────────────────────────────────────

// GoDaddyCredentials is the expected shape of registrar_accounts.credentials
// for GoDaddy accounts.
//
// Field names match exactly what GoDaddy's developer portal labels them:
// "Key" and "Secret" (not "api_key" / "api_secret").
//
// JSON example:
//
//	{ "key": "dKy4G...", "secret": "Sd...", "environment": "production" }
type GoDaddyCredentials struct {
	Key         string `json:"key"`
	Secret      string `json:"secret"`
	Environment string `json:"environment"` // "production" (default) or "ote"
}

// ── Provider ───────────────────────────────────────────────────────────────────

type goDaddyProvider struct {
	creds   GoDaddyCredentials
	baseURL string
	client  *http.Client
}

func newGoDaddyProvider(credentials json.RawMessage) (Provider, error) {
	var creds GoDaddyCredentials
	if err := json.Unmarshal(credentials, &creds); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingCredentials, err)
	}
	if strings.TrimSpace(creds.Key) == "" || strings.TrimSpace(creds.Secret) == "" {
		return nil, fmt.Errorf("%w: key and secret are required", ErrMissingCredentials)
	}

	baseURL := "https://api.godaddy.com"
	if creds.Environment == "ote" {
		baseURL = "https://api.ote-godaddy.com"
	}

	return &goDaddyProvider{
		creds:   creds,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// newGoDaddyProviderWithClient allows injecting a custom HTTP client (for tests).
func newGoDaddyProviderWithClient(creds GoDaddyCredentials, baseURL string, client *http.Client) Provider {
	return &goDaddyProvider{creds: creds, baseURL: baseURL, client: client}
}

func (p *goDaddyProvider) Name() string { return "godaddy" }

func (p *goDaddyProvider) authHeader() string {
	return fmt.Sprintf("sso-key %s:%s", p.creds.Key, p.creds.Secret)
}

// ── Wire types (GoDaddy API response shapes) ───────────────────────────────────

type goDaddyDomainItem struct {
	Domain      string   `json:"domain"`
	CreatedAt   string   `json:"createdAt"`
	Expires     string   `json:"expires"`
	RenewAuto   bool     `json:"renewAuto"`
	Status      string   `json:"status"`
	NameServers []string `json:"nameServers"`
}

// ── ListDomains ────────────────────────────────────────────────────────────────

// ListDomains fetches all domains in the account using cursor-based pagination
// (GoDaddy uses `marker` = last domain name of previous page).
func (p *goDaddyProvider) ListDomains(ctx context.Context) ([]DomainInfo, error) {
	const pageSize = 500
	var all []DomainInfo
	marker := ""

	for {
		url := fmt.Sprintf("%s/v1/domains?limit=%d", p.baseURL, pageSize)
		if marker != "" {
			url += "&marker=" + marker
		}

		items, err := p.fetchDomainList(ctx, url)
		if err != nil {
			return nil, err
		}

		for _, d := range items {
			all = append(all, toDomainInfo(d))
		}

		if len(items) < pageSize {
			break // last page
		}
		marker = items[len(items)-1].Domain
	}

	return all, nil
}

func (p *goDaddyProvider) fetchDomainList(ctx context.Context, url string) ([]goDaddyDomainItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", p.authHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("godaddy list domains: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err := checkStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var items []goDaddyDomainItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("parse domain list: %w", err)
	}
	return items, nil
}

// ── GetDomain ──────────────────────────────────────────────────────────────────

func (p *goDaddyProvider) GetDomain(ctx context.Context, fqdn string) (*DomainInfo, error) {
	url := fmt.Sprintf("%s/v1/domains/%s", p.baseURL, fqdn)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", p.authHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("godaddy get domain: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err := checkStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}

	var d goDaddyDomainItem
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("parse domain: %w", err)
	}

	info := toDomainInfo(d)
	return &info, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func toDomainInfo(d goDaddyDomainItem) DomainInfo {
	info := DomainInfo{
		FQDN:        strings.ToLower(strings.TrimSpace(d.Domain)),
		AutoRenew:   d.RenewAuto,
		Status:      d.Status,
		NameServers: d.NameServers,
	}

	// GoDaddy returns RFC3339 timestamps
	if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
		info.RegistrationDate = &t
	}
	if t, err := time.Parse(time.RFC3339, d.Expires); err == nil {
		info.ExpiryDate = &t
	}

	return info
}

// godaddyErrBody is the JSON shape GoDaddy returns for API errors.
type godaddyErrBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// checkStatus maps GoDaddy HTTP status codes to typed errors.
// For 401/403 it parses GoDaddy's JSON body to distinguish between
// wrong credentials (UNABLE_TO_AUTHENTICATE) and account-level access
// restrictions (ACCESS_DENIED — common for retail accounts after 2023).
func checkStatus(code int, body []byte) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		var apiErr godaddyErrBody
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Code == "ACCESS_DENIED" {
			return fmt.Errorf("%w: %s", ErrAccessDenied, apiErr.Message)
		}
		detail := truncate(string(body), 300)
		return fmt.Errorf("%w: %s", ErrUnauthorized, detail)
	case http.StatusNotFound:
		return ErrDomainNotFound
	case http.StatusTooManyRequests:
		return ErrRateLimitExceeded
	default:
		return fmt.Errorf("godaddy API error %d: %s", code, truncate(string(body), 200))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
