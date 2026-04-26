package cdn

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
	Register("huawei_cdn", NewHuaweiCDNProvider)
}

// huaweiCDNCreds is parsed from the cdn_providers.credentials JSONB.
// Example: {"access_key": "AKID...", "secret_key": "xxx"}
type huaweiCDNCreds struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

const (
	huaweiCDNBaseURL       = "https://cdn.myhuaweicloud.com/v1.0"
	huaweiCDNContentType   = "application/json"
	huaweiCDNIDCacheTTL    = time.Hour
)

// huaweiCDNIDCacheEntry holds a cached domain-name→ID mapping.
type huaweiCDNIDCacheEntry struct {
	id        string
	expiresAt time.Time
}

type huaweiCDNProvider struct {
	signer  *huaweiauth.Signer
	baseURL string
	client  *http.Client
	now     func() int64

	idMu    sync.Mutex
	idCache map[string]huaweiCDNIDCacheEntry
}

// NewHuaweiCDNProvider creates a Huawei Cloud CDN provider from credentials JSON.
// Config JSON is accepted but unused.
func NewHuaweiCDNProvider(config, credentials json.RawMessage) (Provider, error) {
	var creds huaweiCDNCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return nil, fmt.Errorf("%w: access_key and secret_key required", ErrMissingCredentials)
	}
	return &huaweiCDNProvider{
		signer:  huaweiauth.New(creds.AccessKey, creds.SecretKey),
		baseURL: huaweiCDNBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		now:     func() int64 { return time.Now().Unix() },
		idCache: make(map[string]huaweiCDNIDCacheEntry),
	}, nil
}

// newHuaweiCDNProviderWithClient is a test hook that injects a custom HTTP client and base URL.
func newHuaweiCDNProviderWithClient(accessKey, secretKey, baseURL string, client *http.Client) Provider {
	return &huaweiCDNProvider{
		signer:  huaweiauth.New(accessKey, secretKey),
		baseURL: baseURL,
		client:  client,
		now:     func() int64 { return time.Now().Unix() },
		idCache: make(map[string]huaweiCDNIDCacheEntry),
	}
}

func (p *huaweiCDNProvider) Name() string { return "huawei_cdn" }

// ── Wire types ─────────────────────────────────────────────────────────────────

type huaweiCDNOrigin struct {
	OriginType string `json:"origin_type"` // ipaddr | domain | obs_bucket
	IPOrDomain string `json:"ip_or_domain"`
}

type huaweiCDNCreateReq struct {
	Domain struct {
		DomainName   string          `json:"domain_name"`
		BusinessType string          `json:"business_type"` // web | download | video
		Sources      []huaweiCDNOrigin `json:"sources"`
	} `json:"domain"`
}

type huaweiCDNDomain struct {
	ID           string `json:"id"`
	DomainName   string `json:"domain_name"`
	CnameValue   string `json:"cname"`
	DomainStatus string `json:"domain_status"` // online | offline | configuring | checking | configure_failed
	BusinessType string `json:"business_type"`
	CreateTime   int64  `json:"create_time"` // unix milliseconds
}

type huaweiCDNCreateResp struct {
	Domain huaweiCDNDomain `json:"domain"`
}

type huaweiCDNDetailResp struct {
	Domain huaweiCDNDomain `json:"domain"`
}

type huaweiCDNListItem struct {
	ID           string `json:"id"`
	DomainName   string `json:"domain_name"`
	CnameValue   string `json:"cname"`
	DomainStatus string `json:"domain_status"`
	BusinessType string `json:"business_type"`
	CreateTime   int64  `json:"create_time"`
}

type huaweiCDNListResp struct {
	Domains []huaweiCDNListItem `json:"domains"`
	Total   int                 `json:"total"`
}

// huaweiCDNTaskPrefix encodes the task type in the TaskID for GetTaskStatus.
const (
	huaweiCDNRefreshTaskPrefix  = "refresh:"
	huaweiCDNPreheatTaskPrefix  = "preheat:"
)

type huaweiCDNRefreshTaskReq struct {
	RefreshTask struct {
		Type string   `json:"type"` // file | directory
		URLs []string `json:"urls"`
	} `json:"refresh_task"`
}

type huaweiCDNPreheatTaskReq struct {
	PreheatingTask struct {
		URLs []string `json:"urls"`
	} `json:"preheating_task"`
}

type huaweiCDNRefreshTaskResp struct {
	RefreshTask struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"refresh_task"`
}

type huaweiCDNPreheatTaskResp struct {
	PreheatingTask struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"preheating_task"`
}

type huaweiCDNRefreshTaskDetail struct {
	RefreshTask struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		CreateTime int64  `json:"create_time"` // unix ms
		FinishTime int64  `json:"finish_time"` // unix ms; 0 if still running
	} `json:"refresh_task"`
}

type huaweiCDNPreheatTaskDetail struct {
	PreheatingTask struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		CreateTime int64  `json:"create_time"`
		FinishTime int64  `json:"finish_time"`
	} `json:"preheating_task"`
}

// ── Status/type helpers ───────────────────────────────────────────────────────

func huaweiCDNMapStatus(s string) string {
	switch strings.ToLower(s) {
	case "online":
		return DomainStatusOnline
	case "offline":
		return DomainStatusOffline
	case "configuring", "configure_failed":
		return DomainStatusConfiguring
	case "checking":
		return DomainStatusChecking
	default:
		return DomainStatusOffline
	}
}

func huaweiCDNToBusinessType(bt string) string {
	switch bt {
	case BusinessTypeDownload:
		return "download"
	case BusinessTypeMedia:
		return "video"
	default:
		return "web"
	}
}

func huaweiCDNFromBusinessType(bt string) string {
	switch bt {
	case "download":
		return BusinessTypeDownload
	case "video":
		return BusinessTypeMedia
	default:
		return BusinessTypeWeb
	}
}

func huaweiCDNMapTaskStatus(s string) string {
	switch strings.ToLower(s) {
	case "task_done", "success":
		return TaskStatusDone
	case "task_failed", "fail":
		return TaskStatusFailed
	case "task_inited", "waiting":
		return TaskStatusPending
	default:
		return TaskStatusProcessing
	}
}

func huaweiCDNDomainToType(d huaweiCDNDomain) CDNDomain {
	out := CDNDomain{
		Domain:       d.DomainName,
		CNAME:        d.CnameValue,
		Status:       huaweiCDNMapStatus(d.DomainStatus),
		BusinessType: huaweiCDNFromBusinessType(d.BusinessType),
	}
	if d.CreateTime > 0 {
		t := time.UnixMilli(d.CreateTime).UTC()
		out.CreatedAt = &t
	}
	return out
}

// ── ID cache ───────────────────────────────────────────────────────────────────

// resolveDomainID returns the Huawei CDN domain ID for the given domain name,
// using a short-lived cache to avoid extra API calls on repeated operations.
func (p *huaweiCDNProvider) resolveDomainID(ctx context.Context, domainName string) (string, error) {
	p.idMu.Lock()
	entry, ok := p.idCache[domainName]
	now := time.Unix(p.now(), 0)
	if ok && now.Before(entry.expiresAt) {
		p.idMu.Unlock()
		return entry.id, nil
	}
	p.idMu.Unlock()

	// Not cached or stale — query the list API with a name filter.
	url := p.baseURL + "/cdn/domains?domain_name=" + domainName + "&page_size=1&page_number=1"
	var list huaweiCDNListResp
	if err := p.doJSON(ctx, http.MethodGet, url, nil, &list); err != nil {
		return "", fmt.Errorf("huawei_cdn resolve domain id: %w", err)
	}
	if len(list.Domains) == 0 {
		return "", fmt.Errorf("%w: %s", ErrDomainNotFound, domainName)
	}

	id := list.Domains[0].ID
	p.idMu.Lock()
	p.idCache[domainName] = huaweiCDNIDCacheEntry{
		id:        id,
		expiresAt: now.Add(huaweiCDNIDCacheTTL),
	}
	p.idMu.Unlock()
	return id, nil
}

func (p *huaweiCDNProvider) invalidateIDCache(domainName string) {
	p.idMu.Lock()
	delete(p.idCache, domainName)
	p.idMu.Unlock()
}

// ── AddDomain ─────────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error) {
	var createReq huaweiCDNCreateReq
	createReq.Domain.DomainName = req.Domain
	createReq.Domain.BusinessType = huaweiCDNToBusinessType(req.BusinessType)
	createReq.Domain.Sources = make([]huaweiCDNOrigin, 0, len(req.Origins))
	for _, o := range req.Origins {
		originType := "ipaddr"
		if !aliyunIsIP(o.Address) {
			originType = "domain"
		}
		createReq.Domain.Sources = append(createReq.Domain.Sources, huaweiCDNOrigin{
			OriginType: originType,
			IPOrDomain: o.Address,
		})
	}

	var resp huaweiCDNCreateResp
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/cdn/domains", createReq, &resp); err != nil {
		return nil, fmt.Errorf("huawei_cdn add domain: %w", err)
	}

	// Cache the new domain ID.
	p.idMu.Lock()
	p.idCache[req.Domain] = huaweiCDNIDCacheEntry{
		id:        resp.Domain.ID,
		expiresAt: time.Now().Add(huaweiCDNIDCacheTTL),
	}
	p.idMu.Unlock()

	d := huaweiCDNDomainToType(resp.Domain)
	return &d, nil
}

// ── RemoveDomain ──────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) RemoveDomain(ctx context.Context, domain string) error {
	id, err := p.resolveDomainID(ctx, domain)
	if err != nil {
		return err
	}

	// Disable the domain first (required before deletion); ignore errors.
	_ = p.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/cdn/domains/%s/disable", p.baseURL, id), nil, nil)

	if err := p.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/cdn/domains/%s", p.baseURL, id), nil, nil); err != nil {
		return fmt.Errorf("huawei_cdn remove domain: %w", err)
	}
	p.invalidateIDCache(domain)
	return nil
}

// ── GetDomain ─────────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) GetDomain(ctx context.Context, domain string) (*CDNDomain, error) {
	id, err := p.resolveDomainID(ctx, domain)
	if err != nil {
		return nil, err
	}

	var resp huaweiCDNDetailResp
	if err := p.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/cdn/domains/%s/detail", p.baseURL, id), nil, &resp); err != nil {
		return nil, fmt.Errorf("huawei_cdn get domain: %w", err)
	}
	d := huaweiCDNDomainToType(resp.Domain)
	return &d, nil
}

// ── ListDomains ───────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) ListDomains(ctx context.Context) ([]CDNDomain, error) {
	var all []CDNDomain
	pageNum := 1
	pageSize := 100
	for {
		url := fmt.Sprintf("%s/cdn/domains?page_size=%d&page_number=%d", p.baseURL, pageSize, pageNum)
		var resp huaweiCDNListResp
		if err := p.doJSON(ctx, http.MethodGet, url, nil, &resp); err != nil {
			return nil, fmt.Errorf("huawei_cdn list domains: %w", err)
		}
		for _, item := range resp.Domains {
			d := huaweiCDNDomainToType(huaweiCDNDomain{
				ID:           item.ID,
				DomainName:   item.DomainName,
				CnameValue:   item.CnameValue,
				DomainStatus: item.DomainStatus,
				BusinessType: item.BusinessType,
				CreateTime:   item.CreateTime,
			})
			all = append(all, d)
		}
		fetched := (pageNum-1)*pageSize + len(resp.Domains)
		if fetched >= resp.Total || len(resp.Domains) == 0 {
			break
		}
		pageNum++
	}
	return all, nil
}

// ── PurgeURLs ─────────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error) {
	var req huaweiCDNRefreshTaskReq
	req.RefreshTask.Type = "file"
	req.RefreshTask.URLs = urls

	var resp huaweiCDNRefreshTaskResp
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/cdn/content/refresh-tasks", req, &resp); err != nil {
		return nil, fmt.Errorf("huawei_cdn purge urls: %w", err)
	}
	return &PurgeTask{
		TaskID:    huaweiCDNRefreshTaskPrefix + resp.RefreshTask.ID,
		Status:    huaweiCDNMapTaskStatus(resp.RefreshTask.Status),
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PurgeDirectory ────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error) {
	var req huaweiCDNRefreshTaskReq
	req.RefreshTask.Type = "directory"
	req.RefreshTask.URLs = []string{dir}

	var resp huaweiCDNRefreshTaskResp
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/cdn/content/refresh-tasks", req, &resp); err != nil {
		return nil, fmt.Errorf("huawei_cdn purge directory: %w", err)
	}
	return &PurgeTask{
		TaskID:    huaweiCDNRefreshTaskPrefix + resp.RefreshTask.ID,
		Status:    huaweiCDNMapTaskStatus(resp.RefreshTask.Status),
		URLs:      []string{dir},
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PrefetchURLs ──────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) PrefetchURLs(ctx context.Context, urls []string) (*PrefetchTask, error) {
	var req huaweiCDNPreheatTaskReq
	req.PreheatingTask.URLs = urls

	var resp huaweiCDNPreheatTaskResp
	if err := p.doJSON(ctx, http.MethodPost, p.baseURL+"/cdn/content/preheating-tasks", req, &resp); err != nil {
		return nil, fmt.Errorf("huawei_cdn prefetch urls: %w", err)
	}
	return &PrefetchTask{
		TaskID:    huaweiCDNPreheatTaskPrefix + resp.PreheatingTask.ID,
		Status:    huaweiCDNMapTaskStatus(resp.PreheatingTask.Status),
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── GetTaskStatus ─────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error) {
	if strings.HasPrefix(taskID, huaweiCDNRefreshTaskPrefix) {
		rawID := strings.TrimPrefix(taskID, huaweiCDNRefreshTaskPrefix)
		var resp huaweiCDNRefreshTaskDetail
		if err := p.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/cdn/content/refresh-tasks/%s", p.baseURL, rawID), nil, &resp); err != nil {
			return nil, fmt.Errorf("huawei_cdn get refresh task: %w", err)
		}
		s := huaweiCDNMapTaskStatus(resp.RefreshTask.Status)
		out := &TaskStatus{TaskID: taskID, Status: s}
		if resp.RefreshTask.CreateTime > 0 {
			out.CreatedAt = time.UnixMilli(resp.RefreshTask.CreateTime).UTC()
		}
		if (s == TaskStatusDone || s == TaskStatusFailed) && resp.RefreshTask.FinishTime > 0 {
			ft := time.UnixMilli(resp.RefreshTask.FinishTime).UTC()
			out.FinishedAt = &ft
			out.Progress = 100
		}
		return out, nil
	}
	if strings.HasPrefix(taskID, huaweiCDNPreheatTaskPrefix) {
		rawID := strings.TrimPrefix(taskID, huaweiCDNPreheatTaskPrefix)
		var resp huaweiCDNPreheatTaskDetail
		if err := p.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/cdn/content/preheating-tasks/%s", p.baseURL, rawID), nil, &resp); err != nil {
			return nil, fmt.Errorf("huawei_cdn get preheat task: %w", err)
		}
		s := huaweiCDNMapTaskStatus(resp.PreheatingTask.Status)
		out := &TaskStatus{TaskID: taskID, Status: s}
		if resp.PreheatingTask.CreateTime > 0 {
			out.CreatedAt = time.UnixMilli(resp.PreheatingTask.CreateTime).UTC()
		}
		if (s == TaskStatusDone || s == TaskStatusFailed) && resp.PreheatingTask.FinishTime > 0 {
			ft := time.UnixMilli(resp.PreheatingTask.FinishTime).UTC()
			out.FinishedAt = &ft
			out.Progress = 100
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
}

// ── Unsupported ───────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) GetCacheConfig(_ context.Context, _ string) (*CacheConfig, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) SetCacheConfig(_ context.Context, _ string, _ CacheConfig) error {
	return ErrUnsupported
}
func (p *huaweiCDNProvider) GetOriginConfig(_ context.Context, _ string) (*OriginConfig, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) SetOriginConfig(_ context.Context, _ string, _ OriginConfig) error {
	return ErrUnsupported
}
func (p *huaweiCDNProvider) GetAccessControl(_ context.Context, _ string) (*AccessControl, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) SetAccessControl(_ context.Context, _ string, _ AccessControl) error {
	return ErrUnsupported
}
func (p *huaweiCDNProvider) GetHTTPSConfig(_ context.Context, _ string) (*HTTPSConfig, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) SetHTTPSConfig(_ context.Context, _ string, _ HTTPSConfig) error {
	return ErrUnsupported
}
func (p *huaweiCDNProvider) GetPerformanceConfig(_ context.Context, _ string) (*PerformanceConfig, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) SetPerformanceConfig(_ context.Context, _ string, _ PerformanceConfig) error {
	return ErrUnsupported
}
func (p *huaweiCDNProvider) GetBandwidthStats(_ context.Context, _ string, _ StatsRequest) ([]BandwidthPoint, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) GetTrafficStats(_ context.Context, _ string, _ StatsRequest) ([]TrafficPoint, error) {
	return nil, ErrUnsupported
}
func (p *huaweiCDNProvider) GetHitRateStats(_ context.Context, _ string, _ StatsRequest) ([]HitRatePoint, error) {
	return nil, ErrUnsupported
}

// ── HTTP + signing ────────────────────────────────────────────────────────────

func (p *huaweiCDNProvider) doJSON(ctx context.Context, method, url string, body any, dest any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("huawei_cdn marshal request: %w", err)
		}
	}

	headers, err := p.signer.Headers(method, url, bodyBytes, huaweiCDNContentType, p.now())
	if err != nil {
		return fmt.Errorf("huawei_cdn sign request: %w", err)
	}

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("huawei_cdn build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("huawei_cdn request: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err := huaweiCDNCheckHTTP(resp.StatusCode, respBody); err != nil {
		return err
	}
	if dest != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("huawei_cdn parse response: %w", err)
		}
	}
	return nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

func huaweiCDNCheckHTTP(code int, body []byte) error {
	if code == http.StatusOK || code == http.StatusCreated ||
		code == http.StatusAccepted || code == http.StatusNoContent {
		return nil
	}

	var apiErr struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &apiErr)
	errCode := apiErr.Error.Code
	errMsg := apiErr.Error.Message

	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		if errCode != "" {
			return fmt.Errorf("%w: %s %s", ErrUnauthorized, errCode, errMsg)
		}
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, code)
	case http.StatusNotFound:
		if errCode != "" {
			return fmt.Errorf("%w: %s %s", ErrDomainNotFound, errCode, errMsg)
		}
		return fmt.Errorf("%w: HTTP 404", ErrDomainNotFound)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s %s", ErrDomainAlreadyExists, errCode, errMsg)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w", ErrRateLimitExceeded)
	default:
		if errCode != "" {
			return fmt.Errorf("huawei_cdn error %s: %s (HTTP %d)", errCode, errMsg, code)
		}
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("huawei_cdn HTTP %d: %s", code, msg)
	}
}
