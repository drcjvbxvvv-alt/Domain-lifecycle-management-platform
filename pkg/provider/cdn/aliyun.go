package cdn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"domain-platform/pkg/provider/aliyunauth"
)

func init() {
	Register("aliyun_cdn", NewAliyunCDNProvider)
}

// aliyunCDNCreds is parsed from the cdn_providers.credentials JSONB.
// Example: {"access_key_id": "LTAI5t...", "access_key_secret": "..."}
type aliyunCDNCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

const (
	aliyunCDNBaseURL    = "https://cdn.aliyuncs.com"
	aliyunCDNAPIVersion = "2018-05-10"
	aliyunCDNPageSize   = 500
)

type aliyunCDNProvider struct {
	signer  *aliyunauth.Signer
	baseURL string
	client  *http.Client
}

// NewAliyunCDNProvider creates an Aliyun CDN provider from credentials JSON.
// Config JSON is accepted but unused (Aliyun CDN needs no static per-account config).
func NewAliyunCDNProvider(config, credentials json.RawMessage) (Provider, error) {
	var creds aliyunCDNCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.AccessKeyID) == "" || strings.TrimSpace(creds.AccessKeySecret) == "" {
		return nil, fmt.Errorf("%w: access_key_id and access_key_secret required", ErrMissingCredentials)
	}
	return &aliyunCDNProvider{
		signer:  aliyunauth.New(creds.AccessKeyID, creds.AccessKeySecret),
		baseURL: aliyunCDNBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// newAliyunCDNProviderWithClient is a test hook that injects a custom HTTP client and base URL.
func newAliyunCDNProviderWithClient(keyID, keySecret, baseURL string, client *http.Client) Provider {
	return &aliyunCDNProvider{
		signer:  aliyunauth.New(keyID, keySecret),
		baseURL: baseURL,
		client:  client,
	}
}

func (p *aliyunCDNProvider) Name() string { return "aliyun_cdn" }

// ── Wire types ─────────────────────────────────────────────────────────────────

type aliyunCDNDomainDetail struct {
	DomainName   string `json:"DomainName"`
	Cname        string `json:"Cname"`
	DomainStatus string `json:"DomainStatus"`
	CdnType      string `json:"CdnType"`
	GmtCreated   string `json:"GmtCreated"`
}

type aliyunCDNGetDetailResponse struct {
	RequestId            string                `json:"RequestId"`
	GetDomainDetailModel aliyunCDNDomainDetail `json:"GetDomainDetailModel"`
}

type aliyunCDNPageDataItem struct {
	DomainName   string `json:"DomainName"`
	Cname        string `json:"Cname"`
	DomainStatus string `json:"DomainStatus"`
	CdnType      string `json:"CdnType"`
	GmtCreated   string `json:"GmtCreated"`
}

type aliyunCDNDomainsData struct {
	PageData []aliyunCDNPageDataItem `json:"PageData"`
}

type aliyunCDNListDomainsResponse struct {
	RequestId  string              `json:"RequestId"`
	TotalCount int                 `json:"TotalCount"`
	PageNumber int                 `json:"PageNumber"`
	PageSize   int                 `json:"PageSize"`
	Domains    aliyunCDNDomainsData `json:"Domains"`
}

type aliyunCDNRefreshResponse struct {
	RequestId     string `json:"RequestId"`
	RefreshTaskId string `json:"RefreshTaskId"`
}

type aliyunCDNPushResponse struct {
	RequestId  string `json:"RequestId"`
	PushTaskId string `json:"PushTaskId"`
}

type aliyunCDNTaskItem struct {
	TaskId       string `json:"TaskId"`
	ObjectPath   string `json:"ObjectPath"`
	Status       string `json:"Status"`
	Process      string `json:"Process"`   // e.g. "100%"
	ObjectType   string `json:"ObjectType"` // "file" | "directory"
	CreationTime string `json:"CreationTime"`
}

type aliyunCDNTasksData struct {
	CDNTask []aliyunCDNTaskItem `json:"CDNTask"`
}

type aliyunCDNDescribeTasksResponse struct {
	RequestId string             `json:"RequestId"`
	Tasks     aliyunCDNTasksData `json:"Tasks"`
}

type aliyunCDNAPIError struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	RequestId string `json:"RequestId"`
}

// aliyunCDNSource is the origin descriptor serialised into the Sources query param.
type aliyunCDNSource struct {
	Content  string `json:"content"`
	Type     string `json:"type"`            // ipaddr | domain | oss
	Port     int    `json:"port,omitempty"`
	Enabled  bool   `json:"enabled"`
	Weight   string `json:"weight,omitempty"`
	Priority string `json:"priority,omitempty"`
}

// ── Status/type helpers ───────────────────────────────────────────────────────

func aliyunCDNMapStatus(s string) string {
	switch strings.ToLower(s) {
	case "online":
		return DomainStatusOnline
	case "offline":
		return DomainStatusOffline
	case "configuring":
		return DomainStatusConfiguring
	case "checking":
		return DomainStatusChecking
	default:
		return DomainStatusOffline
	}
}

func aliyunCDNToCdnType(bt string) string {
	switch bt {
	case BusinessTypeDownload:
		return "download"
	case BusinessTypeMedia:
		return "video"
	default:
		return "web"
	}
}

func aliyunCDNFromCdnType(cdnType string) string {
	switch cdnType {
	case "download":
		return BusinessTypeDownload
	case "video":
		return BusinessTypeMedia
	default:
		return BusinessTypeWeb
	}
}

func aliyunCDNMapTaskStatus(s string) string {
	switch strings.ToLower(s) {
	case "complete":
		return TaskStatusDone
	case "failed":
		return TaskStatusFailed
	case "pending":
		return TaskStatusPending
	default: // InProgress, etc.
		return TaskStatusProcessing
	}
}

func aliyunCDNDetailToDomain(d aliyunCDNDomainDetail) *CDNDomain {
	out := &CDNDomain{
		Domain:       d.DomainName,
		CNAME:        d.Cname,
		Status:       aliyunCDNMapStatus(d.DomainStatus),
		BusinessType: aliyunCDNFromCdnType(d.CdnType),
	}
	if d.GmtCreated != "" {
		if t, err := time.Parse(time.RFC3339, d.GmtCreated); err == nil {
			out.CreatedAt = &t
		}
	}
	return out
}

// aliyunIsIP returns true if s looks like an IPv4 or IPv6 literal.
func aliyunIsIP(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != ':' {
			return false
		}
	}
	return true
}

// ── AddDomain ─────────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error) {
	params := map[string]string{
		"Action":     "AddCdnDomain",
		"DomainName": req.Domain,
		"CdnType":    aliyunCDNToCdnType(req.BusinessType),
	}

	if len(req.Origins) > 0 {
		sources := make([]aliyunCDNSource, 0, len(req.Origins))
		for _, o := range req.Origins {
			srcType := "ipaddr"
			if !aliyunIsIP(o.Address) {
				srcType = "domain"
			}
			src := aliyunCDNSource{
				Content:  o.Address,
				Type:     srcType,
				Enabled:  true,
				Weight:   "10",
				Priority: "20",
			}
			if o.Port > 0 {
				src.Port = o.Port
			}
			sources = append(sources, src)
		}
		b, err := json.Marshal(sources)
		if err != nil {
			return nil, fmt.Errorf("aliyun_cdn marshal sources: %w", err)
		}
		params["Sources"] = string(b)
	}

	if _, err := p.doRequest(ctx, params); err != nil {
		return nil, fmt.Errorf("aliyun_cdn add domain: %w", err)
	}
	// Fetch the newly created domain to return CNAME and initial status.
	return p.GetDomain(ctx, req.Domain)
}

// ── RemoveDomain ──────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) RemoveDomain(ctx context.Context, domain string) error {
	// Aliyun requires the domain to be offline before deletion; stop it first.
	// Ignore errors — domain may already be offline.
	_, _ = p.doRequest(ctx, map[string]string{
		"Action":     "StopCdnDomain",
		"DomainName": domain,
	})

	if _, err := p.doRequest(ctx, map[string]string{
		"Action":     "DeleteCdnDomain",
		"DomainName": domain,
	}); err != nil {
		return fmt.Errorf("aliyun_cdn remove domain: %w", err)
	}
	return nil
}

// ── GetDomain ─────────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) GetDomain(ctx context.Context, domain string) (*CDNDomain, error) {
	body, err := p.doRequest(ctx, map[string]string{
		"Action":     "DescribeCdnDomainDetail",
		"DomainName": domain,
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn get domain: %w", err)
	}
	var resp aliyunCDNGetDetailResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aliyun_cdn parse domain detail: %w", err)
	}
	return aliyunCDNDetailToDomain(resp.GetDomainDetailModel), nil
}

// ── ListDomains ───────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) ListDomains(ctx context.Context) ([]CDNDomain, error) {
	var all []CDNDomain
	pageNum := 1
	for {
		body, err := p.doRequest(ctx, map[string]string{
			"Action":     "DescribeUserDomains",
			"PageNumber": fmt.Sprintf("%d", pageNum),
			"PageSize":   fmt.Sprintf("%d", aliyunCDNPageSize),
		})
		if err != nil {
			return nil, fmt.Errorf("aliyun_cdn list domains: %w", err)
		}
		var resp aliyunCDNListDomainsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("aliyun_cdn parse list response: %w", err)
		}
		for _, item := range resp.Domains.PageData {
			d := aliyunCDNDetailToDomain(aliyunCDNDomainDetail{
				DomainName:   item.DomainName,
				Cname:        item.Cname,
				DomainStatus: item.DomainStatus,
				CdnType:      item.CdnType,
				GmtCreated:   item.GmtCreated,
			})
			all = append(all, *d)
		}
		fetched := (pageNum-1)*aliyunCDNPageSize + len(resp.Domains.PageData)
		if fetched >= resp.TotalCount || len(resp.Domains.PageData) == 0 {
			break
		}
		pageNum++
	}
	return all, nil
}

// ── PurgeURLs ─────────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error) {
	body, err := p.doRequest(ctx, map[string]string{
		"Action":     "RefreshObjectCaches",
		"ObjectPath": strings.Join(urls, "\n"),
		"ObjectType": "File",
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn purge urls: %w", err)
	}
	var resp aliyunCDNRefreshResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aliyun_cdn parse purge response: %w", err)
	}
	return &PurgeTask{
		TaskID:    resp.RefreshTaskId,
		Status:    TaskStatusProcessing,
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PurgeDirectory ────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error) {
	body, err := p.doRequest(ctx, map[string]string{
		"Action":     "RefreshObjectCaches",
		"ObjectPath": dir,
		"ObjectType": "Directory",
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn purge directory: %w", err)
	}
	var resp aliyunCDNRefreshResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aliyun_cdn parse purge dir response: %w", err)
	}
	return &PurgeTask{
		TaskID:    resp.RefreshTaskId,
		Status:    TaskStatusProcessing,
		URLs:      []string{dir},
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PrefetchURLs ──────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) PrefetchURLs(ctx context.Context, urls []string) (*PrefetchTask, error) {
	body, err := p.doRequest(ctx, map[string]string{
		"Action":     "PushObjectCache",
		"ObjectPath": strings.Join(urls, "\n"),
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn prefetch urls: %w", err)
	}
	var resp aliyunCDNPushResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aliyun_cdn parse prefetch response: %w", err)
	}
	return &PrefetchTask{
		TaskID:    resp.PushTaskId,
		Status:    TaskStatusProcessing,
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── GetTaskStatus ─────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error) {
	body, err := p.doRequest(ctx, map[string]string{
		"Action": "DescribeRefreshTaskById",
		"TaskId": taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn get task status: %w", err)
	}
	var resp aliyunCDNDescribeTasksResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("aliyun_cdn parse task response: %w", err)
	}
	if len(resp.Tasks.CDNTask) == 0 {
		return nil, fmt.Errorf("%w: task %s", ErrTaskNotFound, taskID)
	}

	task := resp.Tasks.CDNTask[0]
	status := aliyunCDNMapTaskStatus(task.Status)
	out := &TaskStatus{
		TaskID: task.TaskId,
		Status: status,
	}

	// Parse "100%" → 100
	if pStr := strings.TrimSuffix(task.Process, "%"); pStr != "" {
		var pct int
		fmt.Sscanf(pStr, "%d", &pct) //nolint:errcheck
		out.Progress = pct
	}
	if task.CreationTime != "" {
		if t, err := time.Parse(time.RFC3339, task.CreationTime); err == nil {
			out.CreatedAt = t
		}
	}
	if status == TaskStatusDone || status == TaskStatusFailed {
		now := time.Now().UTC()
		out.FinishedAt = &now
	}
	return out, nil
}

// ── Unsupported ───────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) GetCacheConfig(_ context.Context, _ string) (*CacheConfig, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) SetCacheConfig(_ context.Context, _ string, _ CacheConfig) error {
	return ErrUnsupported
}
func (p *aliyunCDNProvider) GetOriginConfig(_ context.Context, _ string) (*OriginConfig, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) SetOriginConfig(_ context.Context, _ string, _ OriginConfig) error {
	return ErrUnsupported
}
func (p *aliyunCDNProvider) GetAccessControl(_ context.Context, _ string) (*AccessControl, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) SetAccessControl(_ context.Context, _ string, _ AccessControl) error {
	return ErrUnsupported
}
func (p *aliyunCDNProvider) GetHTTPSConfig(_ context.Context, _ string) (*HTTPSConfig, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) SetHTTPSConfig(_ context.Context, _ string, _ HTTPSConfig) error {
	return ErrUnsupported
}
func (p *aliyunCDNProvider) GetPerformanceConfig(_ context.Context, _ string) (*PerformanceConfig, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) SetPerformanceConfig(_ context.Context, _ string, _ PerformanceConfig) error {
	return ErrUnsupported
}
func (p *aliyunCDNProvider) GetBandwidthStats(_ context.Context, _ string, _ StatsRequest) ([]BandwidthPoint, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) GetTrafficStats(_ context.Context, _ string, _ StatsRequest) ([]TrafficPoint, error) {
	return nil, ErrUnsupported
}
func (p *aliyunCDNProvider) GetHitRateStats(_ context.Context, _ string, _ StatsRequest) ([]HitRatePoint, error) {
	return nil, ErrUnsupported
}

// ── HTTP + signing ────────────────────────────────────────────────────────────

func (p *aliyunCDNProvider) doRequest(ctx context.Context, params map[string]string) ([]byte, error) {
	full := p.signer.CommonParams(aliyunCDNAPIVersion)
	for k, v := range params {
		full[k] = v
	}
	rawURL := p.signer.SignedURL(p.baseURL, full)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aliyun_cdn request: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err := aliyunCDNCheckStatus(resp.StatusCode, body); err != nil {
		return nil, err
	}
	return body, nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

func aliyunCDNCheckStatus(code int, body []byte) error {
	if code == http.StatusOK {
		var apiErr aliyunCDNAPIError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != "" {
			return aliyunCDNMapCode(apiErr.Code, apiErr.Message)
		}
		return nil
	}

	var apiErr aliyunCDNAPIError
	_ = json.Unmarshal(body, &apiErr)

	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		if apiErr.Code != "" {
			return aliyunCDNMapCode(apiErr.Code, apiErr.Message)
		}
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, code)
	case http.StatusNotFound:
		return fmt.Errorf("%w: HTTP 404", ErrDomainNotFound)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w", ErrRateLimitExceeded)
	default:
		if apiErr.Code != "" {
			return aliyunCDNMapCode(apiErr.Code, apiErr.Message)
		}
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("aliyun_cdn HTTP %d: %s", code, msg)
	}
}

func aliyunCDNMapCode(code, message string) error {
	switch code {
	case "InvalidAccessKeyId.NotFound", "InvalidAccessKeyId",
		"SignatureDoesNotMatch", "InvalidAccessKeySecret":
		return fmt.Errorf("%w: %s", ErrUnauthorized, message)
	case "InvalidDomain.NotFound", "InvalidDomainName.NotFound",
		"DomainNotExist", "InvalidDomainName":
		return fmt.Errorf("%w: %s", ErrDomainNotFound, message)
	case "DomainAlreadyExist", "DomainExist":
		return fmt.Errorf("%w: %s", ErrDomainAlreadyExists, message)
	case "Throttling", "ServiceUnavailableTemporary", "Throttling.User":
		return fmt.Errorf("%w: %s", ErrRateLimitExceeded, message)
	default:
		return fmt.Errorf("aliyun_cdn API error %s: %s", code, message)
	}
}
