package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"domain-platform/pkg/provider/tencentauth"
)

func init() {
	Register("dnspod", NewDNSPodProvider)
}

// ── Config / Credentials ──────────────────────────────────────────────────────

// dnspodConfig is parsed from the dns_providers.config JSONB.
// Example: {"domain_name": "example.com"}
type dnspodConfig struct {
	DomainName string `json:"domain_name"`
}

// dnspodCreds is parsed from the dns_providers.credentials JSONB.
// Example: {"secret_id": "AKIDxxx", "secret_key": "yyy"}
type dnspodCreds struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

// ── Provider ──────────────────────────────────────────────────────────────────

const (
	dnspodHost    = "dnspod.tencentcloudapi.com"
	dnspodBaseURL = "https://" + dnspodHost
	dnspodService = "dnspod"
	dnspodVersion = "2021-03-23"

	// dnspodPageLimit is the records-per-page limit safe for all plan tiers.
	// Paid plans support up to 3000; we use 300 to stay within free-plan limits.
	dnspodPageLimit = 300

	// dnspodDefaultLine is the mandatory "line" field in DNSPod record writes.
	// For global/default routing this must be "默认".
	dnspodDefaultLine = "默认"
)

type dnspodProvider struct {
	domainName string // default zone (plain domain name)
	signer     *tencentauth.Signer
	baseURL    string
	client     *http.Client
	now        func() int64 // injectable clock for tests; defaults to time.Now().Unix
}

// NewDNSPodProvider creates a Tencent Cloud DNSPod provider from config and
// credentials JSON.
func NewDNSPodProvider(config, credentials json.RawMessage) (Provider, error) {
	var cfg dnspodConfig
	if err := json.Unmarshal(config, &cfg); err != nil || strings.TrimSpace(cfg.DomainName) == "" {
		return nil, fmt.Errorf("%w: domain_name required in config", ErrMissingConfig)
	}
	var creds dnspodCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.SecretID) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return nil, fmt.Errorf("%w: secret_id and secret_key required", ErrMissingCredentials)
	}

	return &dnspodProvider{
		domainName: cfg.DomainName,
		signer:     tencentauth.New(creds.SecretID, creds.SecretKey),
		baseURL:    dnspodBaseURL,
		client:     &http.Client{Timeout: 30 * time.Second},
		now:        func() int64 { return time.Now().Unix() },
	}, nil
}

// newDNSPodProviderWithClient allows injecting a custom HTTP client, base URL,
// and clock. Used in tests to point at an httptest.Server.
func newDNSPodProviderWithClient(domainName, secretID, secretKey, baseURL string, client *http.Client) Provider {
	return &dnspodProvider{
		domainName: domainName,
		signer:     tencentauth.New(secretID, secretKey),
		baseURL:    baseURL,
		client:     client,
		now:        func() int64 { return time.Now().Unix() },
	}
}

func (p *dnspodProvider) Name() string { return "dnspod" }

// ── Zone resolution ───────────────────────────────────────────────────────────

// resolveZone returns the domain name to use. Falls back to the configured
// domain when the caller passes an empty string.
func (p *dnspodProvider) resolveZone(zone string) string {
	if zone == "" {
		return p.domainName
	}
	return zone
}

// ── Wire types ────────────────────────────────────────────────────────────────

// dnspodRecordItem mirrors one item in DescribeRecordList.RecordList.
// RecordId is uint64 in the Tencent API; we serialise it as a number, then
// convert to string for our Record.ID field.
type dnspodRecordItem struct {
	RecordId uint64 `json:"RecordId"`
	Name     string `json:"Name"`  // subdomain part: "www", "@", etc.
	Type     string `json:"Type"`
	Value    string `json:"Value"`
	TTL      uint32 `json:"TTL"`
	MX       uint32 `json:"MX"`    // priority for MX records (0 otherwise)
	Line     string `json:"Line"`
	Status   string `json:"Status"`
}

type dnspodCountInfo struct {
	TotalCount uint64 `json:"TotalCount"`
	ListedCount uint64 `json:"ListedCount"`
}

// dnspodResponse is the outer envelope for all DNSPod API responses.
// On error the inner Response.Error field is populated.
type dnspodResponse struct {
	Response struct {
		RequestId       string           `json:"RequestId"`
		Error           *dnspodError     `json:"Error,omitempty"`
		// DescribeRecordList
		RecordList      []dnspodRecordItem `json:"RecordList,omitempty"`
		RecordCountInfo *dnspodCountInfo   `json:"RecordCountInfo,omitempty"`
		// CreateRecord / ModifyRecord
		RecordId        *uint64          `json:"RecordId,omitempty"`
		// DescribeDomain
		DomainInfo      *dnspodDomainInfo `json:"DomainInfo,omitempty"`
	} `json:"Response"`
}

type dnspodError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type dnspodDomainInfo struct {
	Domain       string   `json:"Domain"`
	DomainId     uint64   `json:"DomainId"`
	EffectiveDNS []string `json:"EffectiveDNS"`
}

// ── Record conversion ─────────────────────────────────────────────────────────

// dnspodToRecord converts a DNSPod record item to our provider-agnostic Record.
// The Name field in DNSPod is the subdomain part (like Aliyun's RR):
// "www" → "www.example.com", "@" → "example.com".
func dnspodToRecord(r dnspodRecordItem, domain string) Record {
	rec := Record{
		ID:      strconv.FormatUint(r.RecordId, 10),
		Type:    r.Type,
		Name:    nameFromRR(r.Name, domain),
		Content: r.Value,
		TTL:     int(r.TTL),
	}
	if r.Type == RecordTypeMX {
		rec.Priority = int(r.MX)
	}
	return rec
}

// ── List ──────────────────────────────────────────────────────────────────────

func (p *dnspodProvider) ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error) {
	domain := p.resolveZone(zone)
	var all []Record
	offset := uint64(0)

	for {
		req := map[string]any{
			"Domain": domain,
			"Offset": offset,
			"Limit":  uint64(dnspodPageLimit),
		}
		if filter.Type != "" {
			req["RecordType"] = filter.Type
		}
		if filter.Name != "" {
			req["Subdomain"] = rrFromName(filter.Name, domain)
		}

		resp, err := p.call(ctx, "DescribeRecordList", req)
		if err != nil {
			return nil, fmt.Errorf("dnspod list records: %w", err)
		}

		for _, r := range resp.Response.RecordList {
			all = append(all, dnspodToRecord(r, domain))
		}

		// Advance pagination
		fetched := offset + uint64(len(resp.Response.RecordList))
		var total uint64
		if resp.Response.RecordCountInfo != nil {
			total = resp.Response.RecordCountInfo.TotalCount
		}
		if fetched >= total || len(resp.Response.RecordList) == 0 {
			break
		}
		offset = fetched
	}

	return all, nil
}

// ── Create ────────────────────────────────────────────────────────────────────

func (p *dnspodProvider) CreateRecord(ctx context.Context, zone string, record Record) (*Record, error) {
	domain := p.resolveZone(zone)
	subdomain := rrFromName(record.Name, domain)

	req := map[string]any{
		"Domain":     domain,
		"SubDomain":  subdomain,
		"RecordType": record.Type,
		"RecordLine": dnspodDefaultLine,
		"Value":      record.Content,
		"TTL":        record.TTL,
	}
	if record.Type == RecordTypeMX && record.Priority > 0 {
		req["MX"] = record.Priority
	}

	resp, err := p.call(ctx, "CreateRecord", req)
	if err != nil {
		return nil, fmt.Errorf("dnspod create record: %w", err)
	}
	if resp.Response.RecordId == nil {
		return nil, fmt.Errorf("dnspod create: missing RecordId in response")
	}

	out := record
	out.ID = strconv.FormatUint(*resp.Response.RecordId, 10)
	out.Name = nameFromRR(subdomain, domain)
	return &out, nil
}

// ── Update ────────────────────────────────────────────────────────────────────

func (p *dnspodProvider) UpdateRecord(ctx context.Context, zone string, recordID string, record Record) (*Record, error) {
	domain := p.resolveZone(zone)
	subdomain := rrFromName(record.Name, domain)

	id, err := strconv.ParseUint(recordID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("dnspod update: invalid record ID %q: %w", recordID, err)
	}

	req := map[string]any{
		"Domain":     domain,
		"RecordId":   id,
		"SubDomain":  subdomain,
		"RecordType": record.Type,
		"RecordLine": dnspodDefaultLine,
		"Value":      record.Content,
		"TTL":        record.TTL,
	}
	if record.Type == RecordTypeMX && record.Priority > 0 {
		req["MX"] = record.Priority
	}

	_, err = p.call(ctx, "ModifyRecord", req)
	if err != nil {
		return nil, fmt.Errorf("dnspod update record: %w", err)
	}

	out := record
	out.ID = recordID
	out.Name = nameFromRR(subdomain, domain)
	return &out, nil
}

// ── Delete ────────────────────────────────────────────────────────────────────

func (p *dnspodProvider) DeleteRecord(ctx context.Context, zone string, recordID string) error {
	domain := p.resolveZone(zone)

	id, err := strconv.ParseUint(recordID, 10, 64)
	if err != nil {
		return fmt.Errorf("dnspod delete: invalid record ID %q: %w", recordID, err)
	}

	_, err = p.call(ctx, "DeleteRecord", map[string]any{
		"Domain":   domain,
		"RecordId": id,
	})
	if err != nil {
		return fmt.Errorf("dnspod delete record: %w", err)
	}
	return nil
}

// ── GetNameservers ────────────────────────────────────────────────────────────

// GetNameservers returns the effective nameservers for the domain via
// DescribeDomain → DomainInfo.EffectiveDNS.
func (p *dnspodProvider) GetNameservers(ctx context.Context, zone string) ([]string, error) {
	domain := p.resolveZone(zone)

	resp, err := p.call(ctx, "DescribeDomain", map[string]any{"Domain": domain})
	if err != nil {
		return nil, fmt.Errorf("dnspod get nameservers: %w", err)
	}
	if resp.Response.DomainInfo == nil || len(resp.Response.DomainInfo.EffectiveDNS) == 0 {
		return nil, fmt.Errorf("%w: no nameservers returned for %s", ErrZoneNotFound, domain)
	}
	return resp.Response.DomainInfo.EffectiveDNS, nil
}

// ── BatchCreateRecords ────────────────────────────────────────────────────────

func (p *dnspodProvider) BatchCreateRecords(ctx context.Context, zone string, records []Record) ([]Record, error) {
	created := make([]Record, 0, len(records))
	for _, rec := range records {
		r, err := p.CreateRecord(ctx, zone, rec)
		if err != nil {
			return created, fmt.Errorf("batch create %s %s: %w", rec.Type, rec.Name, err)
		}
		created = append(created, *r)
	}
	return created, nil
}

// ── BatchDeleteRecords ────────────────────────────────────────────────────────

func (p *dnspodProvider) BatchDeleteRecords(ctx context.Context, zone string, recordIDs []string) error {
	for _, id := range recordIDs {
		if err := p.DeleteRecord(ctx, zone, id); err != nil {
			return fmt.Errorf("batch delete record %s: %w", id, err)
		}
	}
	return nil
}

// ── HTTP + TC3 signing ────────────────────────────────────────────────────────

// call encodes the request body, adds TC3 headers, POSTs to the DNSPod
// endpoint, and returns the decoded response envelope.
func (p *dnspodProvider) call(ctx context.Context, action string, payload any) (*dnspodResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dnspod marshal request: %w", err)
	}

	// Determine the correct host for signing (strip https:// prefix).
	host := dnspodHost
	if p.baseURL != dnspodBaseURL {
		// In tests the baseURL points at an httptest.Server; use the host part
		// for signing so we don't need to match the test server's host exactly.
		// The TC3 signature in tests is not verified by the mock server.
		host = p.baseURL
	}

	headers := p.signer.Headers(host, dnspodService, action, dnspodVersion, body, p.now())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dnspod build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpResp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dnspod request: %w", err)
	}
	respBody, _ := io.ReadAll(httpResp.Body)
	httpResp.Body.Close()

	if err := dnspodCheckHTTP(httpResp.StatusCode, respBody); err != nil {
		return nil, err
	}

	var resp dnspodResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("dnspod parse response: %w", err)
	}

	if resp.Response.Error != nil {
		return nil, dnspodMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return &resp, nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

// dnspodCheckHTTP maps raw HTTP status codes to sentinel errors before we even
// try to parse the response body. Non-200 responses from Tencent's infra layer
// (load balancers, WAF) may not contain the standard JSON envelope.
func dnspodCheckHTTP(code int, body []byte) error {
	if code == http.StatusOK {
		return nil
	}
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, code)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w", ErrRateLimitExceeded)
	default:
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("dnspod HTTP %d: %s", code, msg)
	}
}

// dnspodMapCode translates a Tencent Cloud API error code to a typed sentinel.
func dnspodMapCode(code, message string) error {
	switch code {
	case "AuthFailure", "AuthFailure.SignatureFailure", "AuthFailure.TokenFailure",
		"AuthFailure.SecretIdNotFound", "AuthFailure.InvalidSecretId":
		return fmt.Errorf("%w: %s", ErrUnauthorized, message)
	case "ResourceNotFound.NoDataOfRecord", "InvalidParameter.RecordIdInvalid":
		return fmt.Errorf("%w: %s", ErrRecordNotFound, message)
	case "InvalidParameter.DomainNotExist", "ResourceNotFound.NoDataOfDomain":
		return fmt.Errorf("%w: %s", ErrZoneNotFound, message)
	case "LimitExceeded.Freq", "RequestLimitExceeded", "LimitExceeded":
		return fmt.Errorf("%w: %s", ErrRateLimitExceeded, message)
	case "InvalidParameter.RecordExistByRecordType":
		return fmt.Errorf("%w: %s", ErrRecordAlreadyExists, message)
	default:
		return fmt.Errorf("dnspod error %s: %s", code, message)
	}
}
