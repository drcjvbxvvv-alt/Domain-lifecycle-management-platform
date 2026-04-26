package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"domain-platform/pkg/provider/huaweiauth"
)

func init() {
	Register("huaweidns", NewHuaweiDNSProvider)
}

// ── Config / Credentials ──────────────────────────────────────────────────────

// hwConfig is parsed from the dns_providers.config JSONB.
// Example: {"domain_name": "example.com"}
type hwConfig struct {
	DomainName string `json:"domain_name"`
}

// hwCreds is parsed from the dns_providers.credentials JSONB.
// Example: {"access_key": "AKID...", "secret_key": "xxx"}
type hwCreds struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// ── Provider ──────────────────────────────────────────────────────────────────

const (
	hwBaseURL    = "https://dns.myhuaweicloud.com"
	hwAPIVersion = "/v2"
	hwPageLimit  = 500

	hwZoneCacheTTL = time.Hour
)

type hwZoneCacheEntry struct {
	zoneID    string
	expiresAt time.Time
}

type hwProvider struct {
	domainName string
	signer     *huaweiauth.Signer
	baseURL    string
	client     *http.Client
	now        func() int64

	zoneMu    sync.Mutex
	zoneCache map[string]hwZoneCacheEntry
}

// NewHuaweiDNSProvider creates a Huawei Cloud DNS provider from config and
// credentials JSON.
func NewHuaweiDNSProvider(config, credentials json.RawMessage) (Provider, error) {
	var cfg hwConfig
	if err := json.Unmarshal(config, &cfg); err != nil || strings.TrimSpace(cfg.DomainName) == "" {
		return nil, fmt.Errorf("%w: domain_name required in config", ErrMissingConfig)
	}
	var creds hwCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return nil, fmt.Errorf("%w: access_key and secret_key required", ErrMissingCredentials)
	}

	return &hwProvider{
		domainName: cfg.DomainName,
		signer:     huaweiauth.New(creds.AccessKey, creds.SecretKey),
		baseURL:    hwBaseURL,
		client:     &http.Client{Timeout: 30 * time.Second},
		now:        func() int64 { return time.Now().Unix() },
		zoneCache:  make(map[string]hwZoneCacheEntry),
	}, nil
}

// newHuaweiDNSProviderWithClient allows injecting a custom HTTP client, base
// URL, and clock. Used in tests to point at an httptest.Server.
func newHuaweiDNSProviderWithClient(domainName, accessKey, secretKey, baseURL string, client *http.Client) Provider {
	return &hwProvider{
		domainName: domainName,
		signer:     huaweiauth.New(accessKey, secretKey),
		baseURL:    baseURL,
		client:     client,
		now:        func() int64 { return time.Now().Unix() },
		zoneCache:  make(map[string]hwZoneCacheEntry),
	}
}

func (p *hwProvider) Name() string { return "huaweidns" }

// ── Zone resolution ───────────────────────────────────────────────────────────

// resolveZoneID returns the Huawei Cloud zone ID for the given domain name.
// Results are cached for hwZoneCacheTTL to avoid redundant API calls.
func (p *hwProvider) resolveZoneID(ctx context.Context, zone string) (string, error) {
	if zone == "" {
		zone = p.domainName
	}
	// Ensure zone ends without a trailing dot for cache key consistency.
	zone = strings.TrimSuffix(zone, ".")

	p.zoneMu.Lock()
	if entry, ok := p.zoneCache[zone]; ok && time.Now().Before(entry.expiresAt) {
		p.zoneMu.Unlock()
		return entry.zoneID, nil
	}
	p.zoneMu.Unlock()

	id, err := p.lookupZoneByName(ctx, zone)
	if err != nil {
		return "", err
	}

	p.zoneMu.Lock()
	p.zoneCache[zone] = hwZoneCacheEntry{
		zoneID:    id,
		expiresAt: time.Now().Add(hwZoneCacheTTL),
	}
	p.zoneMu.Unlock()

	return id, nil
}

// lookupZoneByName calls GET /v2/zones?name={domain}&type=public and returns
// the ID of the first matching zone.
func (p *hwProvider) lookupZoneByName(ctx context.Context, domain string) (string, error) {
	path := hwAPIVersion + "/zones"
	query := "name=" + domain + "&type=public"
	url := p.baseURL + path + "?" + query

	var resp hwZonesResponse
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return "", fmt.Errorf("huaweidns lookup zone %s: %w", domain, err)
	}
	for _, z := range resp.Zones {
		zName := strings.TrimSuffix(z.Name, ".")
		if strings.EqualFold(zName, domain) {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("%w: zone %q not found", ErrZoneNotFound, domain)
}

// ── Wire types ────────────────────────────────────────────────────────────────

type hwZone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Nameservers []string `json:"masters,omitempty"` // secondary zones only
}

type hwZonesResponse struct {
	Zones    []hwZone    `json:"zones"`
	Metadata *hwMetadata `json:"metadata,omitempty"`
}

type hwMetadata struct {
	TotalCount int `json:"total_count"`
}

// hwRecordset mirrors one item in the Huawei Cloud DNS recordsets list.
// Huawei groups all values for a given name+type into one recordset.
// We emit one provider Record per value to stay consistent with other providers.
type hwRecordset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`    // FQDN with trailing dot, e.g. "www.example.com."
	Type    string   `json:"type"`
	Records []string `json:"records"` // one or more values
	TTL     int      `json:"ttl"`
	// MX priority is embedded in value strings: "10 mail.example.com."
}

type hwRecordsetsResponse struct {
	Recordsets []hwRecordset `json:"recordsets"`
	Metadata   *hwMetadata   `json:"metadata,omitempty"`
	// Link markers for pagination
	Links *hwLinks `json:"links,omitempty"`
}

type hwLinks struct {
	Self string `json:"self"`
	Next string `json:"next,omitempty"`
}

type hwRecordsetRequest struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	TTL         int      `json:"ttl"`
	Records     []string `json:"records"`
	Description string   `json:"description,omitempty"`
}

type hwNameserversResponse struct {
	Nameservers []hwNameserver `json:"nameservers"`
}

type hwNameserver struct {
	Hostname string `json:"hostname"`
	Priority int    `json:"priority"`
}

// hwAPIError is returned by Huawei Cloud DNS on failure.
type hwAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Record conversion ─────────────────────────────────────────────────────────

// hwRecordName strips the trailing dot from a Huawei DNS FQDN.
// "www.example.com." → "www.example.com"
func hwRecordName(fqdn string) string {
	return strings.TrimSuffix(fqdn, ".")
}

// hwFQDN adds a trailing dot, as required by Huawei DNS API.
// "www.example.com" → "www.example.com."
func hwFQDN(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// hwRecordsetToRecords converts a single Huawei recordset (which may contain
// multiple values) to one provider Record per value. All returned Records
// share the same recordset ID.
func hwRecordsetToRecords(rs hwRecordset) []Record {
	name := hwRecordName(rs.Name)
	out := make([]Record, 0, len(rs.Records))
	for _, val := range rs.Records {
		rec := Record{
			ID:      rs.ID,
			Type:    rs.Type,
			Name:    name,
			Content: val,
			TTL:     rs.TTL,
		}
		// MX records embed the priority: "10 mail.example.com."
		if rs.Type == RecordTypeMX {
			var prio int
			var target string
			if n, err := fmt.Sscanf(val, "%d %s", &prio, &target); n == 2 && err == nil {
				rec.Priority = prio
				rec.Content = strings.TrimSuffix(target, ".")
			}
		}
		out = append(out, rec)
	}
	return out
}

// hwMXValue formats an MX record value for the Huawei DNS API.
// "mail.example.com", priority 10 → "10 mail.example.com."
func hwMXValue(content string, priority int) string {
	return fmt.Sprintf("%d %s", priority, hwFQDN(content))
}

// hwRecordValue returns the wire value for a record.
// For MX it embeds the priority; for others it passes the content through.
// CNAME and NS targets get a trailing dot.
func hwRecordValue(rec Record) string {
	switch rec.Type {
	case RecordTypeMX:
		return hwMXValue(rec.Content, rec.Priority)
	case "CNAME", "NS":
		return hwFQDN(rec.Content)
	default:
		return rec.Content
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func (p *hwProvider) ListRecords(ctx context.Context, zone string, filter RecordFilter) ([]Record, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	var all []Record
	marker := ""

	for {
		path := fmt.Sprintf("%s/zones/%s/recordsets?limit=%d", hwAPIVersion, zoneID, hwPageLimit)
		if marker != "" {
			path += "&marker=" + marker
		}
		if filter.Type != "" {
			path += "&type=" + filter.Type
		}
		if filter.Name != "" {
			// Huawei name filter takes the FQDN with trailing dot
			path += "&name=" + hwFQDN(filter.Name)
		}

		url := p.baseURL + path

		var resp hwRecordsetsResponse
		if err := p.doJSON(ctx, http.MethodGet, url, nil, &resp); err != nil {
			return nil, fmt.Errorf("huaweidns list records: %w", err)
		}

		for _, rs := range resp.Recordsets {
			all = append(all, hwRecordsetToRecords(rs)...)
		}

		// Pagination: use the marker from the last recordset ID
		if resp.Links == nil || resp.Links.Next == "" || len(resp.Recordsets) == 0 {
			break
		}
		marker = resp.Recordsets[len(resp.Recordsets)-1].ID
	}

	return all, nil
}

// ── Create ────────────────────────────────────────────────────────────────────

func (p *hwProvider) CreateRecord(ctx context.Context, zone string, record Record) (*Record, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	// Huawei DNS requires the record name to be a FQDN with a trailing dot.
	name := hwFQDN(record.Name)

	reqBody := hwRecordsetRequest{
		Name:    name,
		Type:    record.Type,
		TTL:     record.TTL,
		Records: []string{hwRecordValue(record)},
	}

	url := fmt.Sprintf("%s%s/zones/%s/recordsets", p.baseURL, hwAPIVersion, zoneID)

	var rs hwRecordset
	if err := p.doJSON(ctx, http.MethodPost, url, reqBody, &rs); err != nil {
		return nil, fmt.Errorf("huaweidns create record: %w", err)
	}

	records := hwRecordsetToRecords(rs)
	if len(records) == 0 {
		return nil, fmt.Errorf("huaweidns create: empty recordset returned")
	}
	return &records[0], nil
}

// ── Update ────────────────────────────────────────────────────────────────────

func (p *hwProvider) UpdateRecord(ctx context.Context, zone string, recordID string, record Record) (*Record, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	name := hwFQDN(record.Name)
	reqBody := hwRecordsetRequest{
		Name:    name,
		Type:    record.Type,
		TTL:     record.TTL,
		Records: []string{hwRecordValue(record)},
	}

	url := fmt.Sprintf("%s%s/zones/%s/recordsets/%s", p.baseURL, hwAPIVersion, zoneID, recordID)

	var rs hwRecordset
	if err := p.doJSON(ctx, http.MethodPut, url, reqBody, &rs); err != nil {
		return nil, fmt.Errorf("huaweidns update record: %w", err)
	}

	records := hwRecordsetToRecords(rs)
	if len(records) == 0 {
		return nil, fmt.Errorf("huaweidns update: empty recordset returned")
	}
	out := records[0]
	out.ID = recordID
	return &out, nil
}

// ── Delete ────────────────────────────────────────────────────────────────────

func (p *hwProvider) DeleteRecord(ctx context.Context, zone string, recordID string) error {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s%s/zones/%s/recordsets/%s", p.baseURL, hwAPIVersion, zoneID, recordID)

	if err := p.doJSON(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("huaweidns delete record: %w", err)
	}
	return nil
}

// ── GetNameservers ────────────────────────────────────────────────────────────

// GetNameservers returns the nameservers for a zone via
// GET /v2/nameservers?zone_id={zone_id}.
func (p *hwProvider) GetNameservers(ctx context.Context, zone string) ([]string, error) {
	zoneID, err := p.resolveZoneID(ctx, zone)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s%s/nameservers?zone_id=%s", p.baseURL, hwAPIVersion, zoneID)

	var resp hwNameserversResponse
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, fmt.Errorf("huaweidns get nameservers: %w", err)
	}
	if len(resp.Nameservers) == 0 {
		return nil, fmt.Errorf("%w: no nameservers returned for zone %s", ErrZoneNotFound, zoneID)
	}

	ns := make([]string, len(resp.Nameservers))
	for i, n := range resp.Nameservers {
		ns[i] = strings.TrimSuffix(n.Hostname, ".")
	}
	return ns, nil
}

// ── BatchCreateRecords ────────────────────────────────────────────────────────

func (p *hwProvider) BatchCreateRecords(ctx context.Context, zone string, records []Record) ([]Record, error) {
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

func (p *hwProvider) BatchDeleteRecords(ctx context.Context, zone string, recordIDs []string) error {
	for _, id := range recordIDs {
		if err := p.DeleteRecord(ctx, zone, id); err != nil {
			return fmt.Errorf("batch delete record %s: %w", id, err)
		}
	}
	return nil
}

// ── HTTP transport ────────────────────────────────────────────────────────────

// doJSON sends an HTTP request with AK/SK signing, decodes the JSON response
// into dest (can be nil for DELETE), and maps API errors to sentinels.
func (p *hwProvider) doJSON(ctx context.Context, method, url string, body any, dest any) error {
	const contentType = "application/json"

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("huaweidns marshal request: %w", err)
		}
	}

	headers, err := p.signer.Headers(method, url, bodyBytes, contentType, p.now())
	if err != nil {
		return fmt.Errorf("huaweidns sign request: %w", err)
	}

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("huaweidns build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("huaweidns request: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err := hwCheckHTTP(resp.StatusCode, respBody); err != nil {
		return err
	}

	if dest != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("huaweidns parse response: %w", err)
		}
	}
	return nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

// hwCheckHTTP maps HTTP status codes + error body to sentinel errors.
func hwCheckHTTP(code int, body []byte) error {
	// 200 OK and 204 No Content are both success.
	if code == http.StatusOK || code == http.StatusNoContent || code == http.StatusAccepted {
		return nil
	}

	// Try to parse a Huawei API error envelope for richer diagnostics.
	var apiErr struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &apiErr)

	if apiErr.Code != "" {
		return hwMapCode(apiErr.Code, apiErr.Message)
	}

	// Fallback: map by HTTP status
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, code)
	case http.StatusNotFound:
		return fmt.Errorf("%w: HTTP 404", ErrRecordNotFound)
	case http.StatusConflict:
		return fmt.Errorf("%w: HTTP 409", ErrRecordAlreadyExists)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w", ErrRateLimitExceeded)
	default:
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("huaweidns HTTP %d: %s", code, msg)
	}
}

// hwMapCode translates a Huawei Cloud DNS API error code to a typed sentinel.
func hwMapCode(code, message string) error {
	switch {
	case code == "DNS.0101" || strings.HasPrefix(code, "APIG.") ||
		code == "DNS.0112" || code == "DNS.0103":
		// Authentication / authorisation failures
		return fmt.Errorf("%w: %s", ErrUnauthorized, message)
	case code == "DNS.0601" || code == "DNS.0602" || code == "DNS.0603":
		// Record not found / does not exist
		return fmt.Errorf("%w: %s", ErrRecordNotFound, message)
	case code == "DNS.0401" || code == "DNS.0403":
		// Zone not found
		return fmt.Errorf("%w: %s", ErrZoneNotFound, message)
	case code == "DNS.0403":
		return fmt.Errorf("%w: %s", ErrZoneNotFound, message)
	case strings.HasPrefix(code, "DNS.05"):
		// Rate limit / quota exceeded
		return fmt.Errorf("%w: %s", ErrRateLimitExceeded, message)
	case code == "DNS.0417":
		// Recordset already exists
		return fmt.Errorf("%w: %s", ErrRecordAlreadyExists, message)
	default:
		return fmt.Errorf("huaweidns error %s: %s", code, message)
	}
}
