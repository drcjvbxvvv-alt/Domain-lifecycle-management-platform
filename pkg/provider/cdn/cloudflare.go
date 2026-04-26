package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("cloudflare_cdn", NewCloudflareCDNProvider)
}

// cloudflareCDNCreds is parsed from the cdn_providers.credentials JSONB.
// Example: {"api_token": "Bearer token", "zone_id": "abc123..."}
type cloudflareCDNCreds struct {
	APIToken string `json:"api_token"`
	ZoneID   string `json:"zone_id"`
}

const (
	cloudflareCDNBaseURL    = "https://api.cloudflare.com/client/v4"
	cloudflareCDNTaskPrefix = "cf-purge:"
)

type cloudflareCDNProvider struct {
	token   string
	zoneID  string
	baseURL string
	client  *http.Client
}

// NewCloudflareCDNProvider creates a Cloudflare CDN provider from credentials JSON.
//
// Cloudflare CDN works differently from traditional CDN providers: "adding a CDN
// domain" means creating or updating a proxied (orange-cloud) DNS record in the
// zone. There is no separate CDN domain concept — Cloudflare's CDN sits in front
// of DNS records with proxied=true.
func NewCloudflareCDNProvider(config, credentials json.RawMessage) (Provider, error) {
	var creds cloudflareCDNCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.APIToken) == "" || strings.TrimSpace(creds.ZoneID) == "" {
		return nil, fmt.Errorf("%w: api_token and zone_id required", ErrMissingCredentials)
	}
	return &cloudflareCDNProvider{
		token:   creds.APIToken,
		zoneID:  creds.ZoneID,
		baseURL: cloudflareCDNBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// newCloudflareCDNProviderWithClient is a test hook that injects a custom HTTP client and base URL.
func newCloudflareCDNProviderWithClient(token, zoneID, baseURL string, client *http.Client) Provider {
	return &cloudflareCDNProvider{
		token:   token,
		zoneID:  zoneID,
		baseURL: baseURL,
		client:  client,
	}
}

func (p *cloudflareCDNProvider) Name() string { return "cloudflare_cdn" }

// ── Wire types ─────────────────────────────────────────────────────────────────

type cfDNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Proxied  bool   `json:"proxied"`
	TTL      int    `json:"ttl,omitempty"` // 1 = auto
}

type cfAPIResult struct {
	Success  bool            `json:"success"`
	Errors   []cfAPIError    `json:"errors"`
	Result   json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfPurgeCacheReq struct {
	Files    []string `json:"files,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`
}

type cfPurgeCacheResult struct {
	ID string `json:"id"`
}

// ── AddDomain ─────────────────────────────────────────────────────────────────

// AddDomain creates (or updates) a proxied DNS record in the Cloudflare zone.
// The first origin in req.Origins is used as the A-record target; if no origins
// are provided, a CNAME pointing to the domain itself is created as a placeholder.
func (p *cloudflareCDNProvider) AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error) {
	recType := "CNAME"
	content := req.Domain // placeholder
	if len(req.Origins) > 0 {
		if aliyunIsIP(req.Origins[0].Address) {
			recType = "A"
		} else {
			recType = "CNAME"
		}
		content = req.Origins[0].Address
	}

	record := cfDNSRecord{
		Type:    recType,
		Name:    req.Domain,
		Content: content,
		Proxied: true,
		TTL:     1, // auto
	}

	// Try to find an existing record; update if found, create otherwise.
	existingID, _ := p.findRecordID(ctx, req.Domain)
	var rawResult json.RawMessage
	if existingID != "" {
		url := fmt.Sprintf("%s/zones/%s/dns_records/%s", p.baseURL, p.zoneID, existingID)
		if err := p.doJSON(ctx, http.MethodPut, url, record, &rawResult); err != nil {
			return nil, fmt.Errorf("cloudflare_cdn update record: %w", err)
		}
	} else {
		url := fmt.Sprintf("%s/zones/%s/dns_records", p.baseURL, p.zoneID)
		if err := p.doJSON(ctx, http.MethodPost, url, record, &rawResult); err != nil {
			return nil, fmt.Errorf("cloudflare_cdn create record: %w", err)
		}
	}

	return &CDNDomain{
		Domain:       req.Domain,
		CNAME:        "", // Cloudflare proxied records have no external CNAME
		Status:       DomainStatusOnline,
		BusinessType: req.BusinessType,
	}, nil
}

// ── RemoveDomain ──────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) RemoveDomain(ctx context.Context, domain string) error {
	id, err := p.findRecordID(ctx, domain)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", p.baseURL, p.zoneID, id)
	if err := p.doJSON(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("cloudflare_cdn remove record: %w", err)
	}
	return nil
}

// ── GetDomain ─────────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) GetDomain(ctx context.Context, domain string) (*CDNDomain, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", p.baseURL, p.zoneID, domain)
	var rawResult json.RawMessage
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &rawResult); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn get domain: %w", err)
	}

	var records []cfDNSRecord
	if err := json.Unmarshal(rawResult, &records); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn parse records: %w", err)
	}
	for _, r := range records {
		if strings.EqualFold(r.Name, domain) {
			status := DomainStatusOffline
			if r.Proxied {
				status = DomainStatusOnline
			}
			return &CDNDomain{
				Domain: domain,
				CNAME:  "",
				Status: status,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrDomainNotFound, domain)
}

// ── ListDomains ───────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) ListDomains(ctx context.Context) ([]CDNDomain, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?per_page=1000", p.baseURL, p.zoneID)
	var rawResult json.RawMessage
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &rawResult); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn list domains: %w", err)
	}

	var records []cfDNSRecord
	if err := json.Unmarshal(rawResult, &records); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn parse records: %w", err)
	}

	var all []CDNDomain
	for _, r := range records {
		if !r.Proxied {
			continue
		}
		all = append(all, CDNDomain{
			Domain: r.Name,
			CNAME:  "",
			Status: DomainStatusOnline,
		})
	}
	return all, nil
}

// ── PurgeURLs ─────────────────────────────────────────────────────────────────

// PurgeURLs purges specific URLs from Cloudflare's cache. Cloudflare cache purge
// is synchronous; the returned task reflects the immediate result.
func (p *cloudflareCDNProvider) PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error) {
	url := fmt.Sprintf("%s/zones/%s/purge_cache", p.baseURL, p.zoneID)
	var rawResult json.RawMessage
	if err := p.doJSON(ctx, http.MethodPost, url, cfPurgeCacheReq{Files: urls}, &rawResult); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn purge urls: %w", err)
	}
	var res cfPurgeCacheResult
	_ = json.Unmarshal(rawResult, &res)

	taskID := cloudflareCDNTaskPrefix + res.ID
	if res.ID == "" {
		taskID = cloudflareCDNTaskPrefix + "sync"
	}
	return &PurgeTask{
		TaskID:    taskID,
		Status:    TaskStatusDone,
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PurgeDirectory ────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error) {
	url := fmt.Sprintf("%s/zones/%s/purge_cache", p.baseURL, p.zoneID)
	var rawResult json.RawMessage
	if err := p.doJSON(ctx, http.MethodPost, url, cfPurgeCacheReq{Prefixes: []string{dir}}, &rawResult); err != nil {
		return nil, fmt.Errorf("cloudflare_cdn purge directory: %w", err)
	}
	var res cfPurgeCacheResult
	_ = json.Unmarshal(rawResult, &res)

	taskID := cloudflareCDNTaskPrefix + res.ID
	if res.ID == "" {
		taskID = cloudflareCDNTaskPrefix + "sync"
	}
	return &PurgeTask{
		TaskID:    taskID,
		Status:    TaskStatusDone,
		URLs:      []string{dir},
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PrefetchURLs ──────────────────────────────────────────────────────────────

// PrefetchURLs is not supported by Cloudflare.
func (p *cloudflareCDNProvider) PrefetchURLs(_ context.Context, _ []string) (*PrefetchTask, error) {
	return nil, ErrUnsupported
}

// ── GetTaskStatus ─────────────────────────────────────────────────────────────

// GetTaskStatus returns a synthetic "done" status for Cloudflare purge tasks.
// Cloudflare cache purge is synchronous; there is no async task to poll.
func (p *cloudflareCDNProvider) GetTaskStatus(_ context.Context, taskID string) (*TaskStatus, error) {
	if !strings.HasPrefix(taskID, cloudflareCDNTaskPrefix) {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	now := time.Now().UTC()
	return &TaskStatus{
		TaskID:     taskID,
		Status:     TaskStatusDone,
		Progress:   100,
		CreatedAt:  now,
		FinishedAt: &now,
	}, nil
}

// ── Unsupported ───────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) GetCacheConfig(_ context.Context, _ string) (*CacheConfig, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) SetCacheConfig(_ context.Context, _ string, _ CacheConfig) error {
	return ErrUnsupported
}
func (p *cloudflareCDNProvider) GetOriginConfig(_ context.Context, _ string) (*OriginConfig, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) SetOriginConfig(_ context.Context, _ string, _ OriginConfig) error {
	return ErrUnsupported
}
func (p *cloudflareCDNProvider) GetAccessControl(_ context.Context, _ string) (*AccessControl, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) SetAccessControl(_ context.Context, _ string, _ AccessControl) error {
	return ErrUnsupported
}
func (p *cloudflareCDNProvider) GetHTTPSConfig(_ context.Context, _ string) (*HTTPSConfig, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) SetHTTPSConfig(_ context.Context, _ string, _ HTTPSConfig) error {
	return ErrUnsupported
}
func (p *cloudflareCDNProvider) GetPerformanceConfig(_ context.Context, _ string) (*PerformanceConfig, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) SetPerformanceConfig(_ context.Context, _ string, _ PerformanceConfig) error {
	return ErrUnsupported
}
func (p *cloudflareCDNProvider) GetBandwidthStats(_ context.Context, _ string, _ StatsRequest) ([]BandwidthPoint, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) GetTrafficStats(_ context.Context, _ string, _ StatsRequest) ([]TrafficPoint, error) {
	return nil, ErrUnsupported
}
func (p *cloudflareCDNProvider) GetHitRateStats(_ context.Context, _ string, _ StatsRequest) ([]HitRatePoint, error) {
	return nil, ErrUnsupported
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// findRecordID looks up the DNS record ID for the given hostname in the zone.
func (p *cloudflareCDNProvider) findRecordID(ctx context.Context, domain string) (string, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", p.baseURL, p.zoneID, domain)
	var rawResult json.RawMessage
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &rawResult); err != nil {
		return "", err
	}
	var records []cfDNSRecord
	if err := json.Unmarshal(rawResult, &records); err != nil {
		return "", fmt.Errorf("cloudflare_cdn parse records: %w", err)
	}
	for _, r := range records {
		if strings.EqualFold(r.Name, domain) {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrDomainNotFound, domain)
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

func (p *cloudflareCDNProvider) doJSON(ctx context.Context, method, url string, body any, dest any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloudflare_cdn marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("cloudflare_cdn build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare_cdn request: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Cloudflare always returns 200 with success:false on API errors.
	var apiResult cfAPIResult
	if err := json.Unmarshal(respBody, &apiResult); err != nil {
		// Non-JSON response — check HTTP status.
		if resp.StatusCode >= 400 {
			return fmt.Errorf("cloudflare_cdn HTTP %d: %s", resp.StatusCode, string(respBody))
		}
		return nil
	}

	if !apiResult.Success {
		return cloudflareMapErrors(apiResult.Errors)
	}
	if dest != nil && apiResult.Result != nil {
		if err := json.Unmarshal(apiResult.Result, dest); err != nil {
			return fmt.Errorf("cloudflare_cdn parse result: %w", err)
		}
	}
	return nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

func cloudflareMapErrors(errs []cfAPIError) error {
	if len(errs) == 0 {
		return fmt.Errorf("cloudflare_cdn: API returned success=false with no error details")
	}
	// Map the first error code to a sentinel if possible; chain the rest as context.
	first := errs[0]
	switch first.Code {
	case 10000, 10013: // Authentication / zone not authorised
		return fmt.Errorf("%w: %s", ErrUnauthorized, first.Message)
	case 81053: // Record already exists
		return fmt.Errorf("%w: %s", ErrDomainAlreadyExists, first.Message)
	case 81044, 81058, 7003: // Not found
		return fmt.Errorf("%w: %s", ErrDomainNotFound, first.Message)
	default:
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, fmt.Sprintf("%d: %s", e.Code, e.Message))
		}
		return fmt.Errorf("cloudflare_cdn API error: %s", strings.Join(msgs, "; "))
	}
}
