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

	"domain-platform/pkg/provider/tencentauth"
)

func init() {
	Register("tencent_cdn", NewTencentCDNProvider)
}

// tencentCDNCreds is parsed from the cdn_providers.credentials JSONB.
// Example: {"secret_id": "AKIDxxx", "secret_key": "yyy"}
type tencentCDNCreds struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

const (
	tencentCDNHost    = "cdn.tencentcloudapi.com"
	tencentCDNBaseURL = "https://" + tencentCDNHost
	tencentCDNService = "cdn"
	tencentCDNVersion = "2018-06-06"

	// tencentCDNTaskPurgePrefix and tencentCDNTaskPushPrefix are encoded into
	// TaskID so GetTaskStatus knows which API to query.
	tencentCDNTaskPurgePrefix = "purge:"
	tencentCDNTaskPushPrefix  = "push:"

	tencentCDNPageSize = 100
)

type tencentCDNProvider struct {
	signer  *tencentauth.Signer
	baseURL string
	client  *http.Client
	now     func() int64
}

// NewTencentCDNProvider creates a Tencent Cloud CDN provider from credentials JSON.
// Config JSON is accepted but unused.
func NewTencentCDNProvider(config, credentials json.RawMessage) (Provider, error) {
	var creds tencentCDNCreds
	if err := json.Unmarshal(credentials, &creds); err != nil ||
		strings.TrimSpace(creds.SecretID) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return nil, fmt.Errorf("%w: secret_id and secret_key required", ErrMissingCredentials)
	}
	return &tencentCDNProvider{
		signer:  tencentauth.New(creds.SecretID, creds.SecretKey),
		baseURL: tencentCDNBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
		now:     func() int64 { return time.Now().Unix() },
	}, nil
}

// newTencentCDNProviderWithClient is a test hook that injects a custom HTTP client and base URL.
func newTencentCDNProviderWithClient(secretID, secretKey, baseURL string, client *http.Client) Provider {
	return &tencentCDNProvider{
		signer:  tencentauth.New(secretID, secretKey),
		baseURL: baseURL,
		client:  client,
		now:     func() int64 { return time.Now().Unix() },
	}
}

func (p *tencentCDNProvider) Name() string { return "tencent_cdn" }

// ── Wire types ─────────────────────────────────────────────────────────────────

type tencentCDNOrigin struct {
	Origins            []string `json:"Origins"`
	OriginType         string   `json:"OriginType"` // ip | domain | cos | third_party
	OriginPullProtocol string   `json:"OriginPullProtocol,omitempty"`
}

type tencentCDNAddDomainReq struct {
	Domain      string           `json:"Domain"`
	ServiceType string           `json:"ServiceType"` // web | download | media
	Origin      tencentCDNOrigin `json:"Origin"`
}

type tencentCDNAddDomainReqNoOrigin struct {
	Domain      string `json:"Domain"`
	ServiceType string `json:"ServiceType"`
}

type tencentCDNFilter struct {
	Name  string   `json:"Name"`
	Value []string `json:"Value"`
}

type tencentCDNListReq struct {
	Filters []tencentCDNFilter `json:"Filters,omitempty"`
	Limit   int                `json:"Limit"`
	Offset  int                `json:"Offset"`
}

type tencentCDNDomainItem struct {
	Domain      string `json:"Domain"`
	Cname       string `json:"Cname"`
	Status      string `json:"Status"`
	ServiceType string `json:"ServiceType"`
	CreateTime  string `json:"CreateTime"`
}

type tencentCDNError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type tencentCDNListResp struct {
	Response struct {
		Domains     []tencentCDNDomainItem `json:"Domains"`
		TotalNumber int                    `json:"TotalNumber"`
		RequestId   string                 `json:"RequestId"`
		Error       *tencentCDNError       `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentCDNBasicResp struct {
	Response struct {
		RequestId string           `json:"RequestId"`
		Error     *tencentCDNError `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentCDNTaskIDResp struct {
	Response struct {
		TaskId    string           `json:"TaskId"`
		RequestId string           `json:"RequestId"`
		Error     *tencentCDNError `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentCDNPurgeLog struct {
	TaskId       string `json:"TaskId"`
	Url          string `json:"Url"`
	Status       string `json:"Status"`
	CreateTime   string `json:"CreateTime"`
	UpdateTime   string `json:"UpdateTime"`
}

type tencentCDNPurgeTasksResp struct {
	Response struct {
		PurgeLogs  []tencentCDNPurgeLog `json:"PurgeLogs"`
		TotalCount int                  `json:"TotalCount"`
		RequestId  string               `json:"RequestId"`
		Error      *tencentCDNError     `json:"Error,omitempty"`
	} `json:"Response"`
}

type tencentCDNPushLog struct {
	TaskId     string `json:"TaskId"`
	Url        string `json:"Url"`
	Status     string `json:"Status"`
	CreateTime string `json:"CreateTime"`
	UpdateTime string `json:"UpdateTime"`
}

type tencentCDNPushTasksResp struct {
	Response struct {
		PushLogs   []tencentCDNPushLog `json:"PushLogs"`
		TotalCount int                 `json:"TotalCount"`
		RequestId  string              `json:"RequestId"`
		Error      *tencentCDNError    `json:"Error,omitempty"`
	} `json:"Response"`
}

// ── Status/type helpers ───────────────────────────────────────────────────────

func tencentCDNMapStatus(s string) string {
	switch strings.ToLower(s) {
	case "online":
		return DomainStatusOnline
	case "offline":
		return DomainStatusOffline
	case "processing", "submitted", "deploying":
		return DomainStatusConfiguring
	default:
		return DomainStatusOffline
	}
}

func tencentCDNMapTaskStatus(s string) string {
	switch strings.ToLower(s) {
	case "done", "success":
		return TaskStatusDone
	case "fail", "failed":
		return TaskStatusFailed
	case "init", "wait":
		return TaskStatusPending
	default:
		return TaskStatusProcessing
	}
}

func tencentCDNDomainToCDN(item tencentCDNDomainItem) CDNDomain {
	d := CDNDomain{
		Domain:       item.Domain,
		CNAME:        item.Cname,
		Status:       tencentCDNMapStatus(item.Status),
		BusinessType: item.ServiceType,
	}
	if item.CreateTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", item.CreateTime); err == nil {
			d.CreatedAt = &t
		}
	}
	return d
}

// ── AddDomain ─────────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error) {
	var payload any
	if len(req.Origins) > 0 {
		origins := make([]string, 0, len(req.Origins))
		for _, o := range req.Origins {
			origins = append(origins, o.Address)
		}
		originType := "ip"
		if !aliyunIsIP(req.Origins[0].Address) {
			originType = "domain"
		}
		payload = tencentCDNAddDomainReq{
			Domain:      req.Domain,
			ServiceType: req.BusinessType,
			Origin: tencentCDNOrigin{
				Origins:            origins,
				OriginType:         originType,
				OriginPullProtocol: "follow",
			},
		}
	} else {
		payload = tencentCDNAddDomainReqNoOrigin{
			Domain:      req.Domain,
			ServiceType: req.BusinessType,
		}
	}

	var resp tencentCDNBasicResp
	if err := p.call(ctx, "AddCdnDomain", payload, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn add domain: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}

	// Fetch the created domain to get CNAME.
	return p.GetDomain(ctx, req.Domain)
}

// ── RemoveDomain ──────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) RemoveDomain(ctx context.Context, domain string) error {
	// Stop the domain first (required before deletion). Ignore errors — may already be offline.
	var stopResp tencentCDNBasicResp
	_ = p.call(ctx, "StopCdnDomain", map[string]string{"Domain": domain}, &stopResp)

	var resp tencentCDNBasicResp
	if err := p.call(ctx, "DeleteCdnDomain", map[string]string{"Domain": domain}, &resp); err != nil {
		return fmt.Errorf("tencent_cdn remove domain: %w", err)
	}
	if resp.Response.Error != nil {
		return tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return nil
}

// ── GetDomain ─────────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) GetDomain(ctx context.Context, domain string) (*CDNDomain, error) {
	req := tencentCDNListReq{
		Filters: []tencentCDNFilter{
			{Name: "domain", Value: []string{domain}},
		},
		Limit:  1,
		Offset: 0,
	}
	var resp tencentCDNListResp
	if err := p.call(ctx, "DescribeDomains", req, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn get domain: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	if len(resp.Response.Domains) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrDomainNotFound, domain)
	}
	d := tencentCDNDomainToCDN(resp.Response.Domains[0])
	return &d, nil
}

// ── ListDomains ───────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) ListDomains(ctx context.Context) ([]CDNDomain, error) {
	var all []CDNDomain
	offset := 0
	for {
		req := tencentCDNListReq{Limit: tencentCDNPageSize, Offset: offset}
		var resp tencentCDNListResp
		if err := p.call(ctx, "DescribeDomains", req, &resp); err != nil {
			return nil, fmt.Errorf("tencent_cdn list domains: %w", err)
		}
		if resp.Response.Error != nil {
			return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
		}
		for _, item := range resp.Response.Domains {
			d := tencentCDNDomainToCDN(item)
			all = append(all, d)
		}
		offset += len(resp.Response.Domains)
		if offset >= resp.Response.TotalNumber || len(resp.Response.Domains) == 0 {
			break
		}
	}
	return all, nil
}

// ── PurgeURLs ─────────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error) {
	var resp tencentCDNTaskIDResp
	if err := p.call(ctx, "PurgeUrlsCache", map[string][]string{"Urls": urls}, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn purge urls: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return &PurgeTask{
		TaskID:    tencentCDNTaskPurgePrefix + resp.Response.TaskId,
		Status:    TaskStatusProcessing,
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PurgeDirectory ────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error) {
	payload := map[string]any{
		"Dirs":      []string{dir},
		"FlushType": "delete",
	}
	var resp tencentCDNTaskIDResp
	if err := p.call(ctx, "PurgePathCache", payload, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn purge directory: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return &PurgeTask{
		TaskID:    tencentCDNTaskPurgePrefix + resp.Response.TaskId,
		Status:    TaskStatusProcessing,
		URLs:      []string{dir},
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── PrefetchURLs ──────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) PrefetchURLs(ctx context.Context, urls []string) (*PrefetchTask, error) {
	var resp tencentCDNTaskIDResp
	if err := p.call(ctx, "PushUrlsCache", map[string][]string{"Urls": urls}, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn prefetch urls: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	return &PrefetchTask{
		TaskID:    tencentCDNTaskPushPrefix + resp.Response.TaskId,
		Status:    TaskStatusProcessing,
		URLs:      urls,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ── GetTaskStatus ─────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error) {
	if strings.HasPrefix(taskID, tencentCDNTaskPurgePrefix) {
		return p.getPurgeTaskStatus(ctx, strings.TrimPrefix(taskID, tencentCDNTaskPurgePrefix))
	}
	if strings.HasPrefix(taskID, tencentCDNTaskPushPrefix) {
		return p.getPushTaskStatus(ctx, strings.TrimPrefix(taskID, tencentCDNTaskPushPrefix))
	}
	return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
}

func (p *tencentCDNProvider) getPurgeTaskStatus(ctx context.Context, rawID string) (*TaskStatus, error) {
	payload := map[string]any{"TaskId": rawID, "Limit": 1, "Offset": 0}
	var resp tencentCDNPurgeTasksResp
	if err := p.call(ctx, "DescribePurgeTasks", payload, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn get purge task: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	if len(resp.Response.PurgeLogs) == 0 {
		return nil, fmt.Errorf("%w: task %s", ErrTaskNotFound, rawID)
	}
	log := resp.Response.PurgeLogs[0]
	return tencentCDNBuildTaskStatus(tencentCDNTaskPurgePrefix+log.TaskId, log.Status, log.CreateTime, log.UpdateTime), nil
}

func (p *tencentCDNProvider) getPushTaskStatus(ctx context.Context, rawID string) (*TaskStatus, error) {
	payload := map[string]any{"TaskId": rawID, "Limit": 1, "Offset": 0}
	var resp tencentCDNPushTasksResp
	if err := p.call(ctx, "DescribePushTasks", payload, &resp); err != nil {
		return nil, fmt.Errorf("tencent_cdn get push task: %w", err)
	}
	if resp.Response.Error != nil {
		return nil, tencentCDNMapCode(resp.Response.Error.Code, resp.Response.Error.Message)
	}
	if len(resp.Response.PushLogs) == 0 {
		return nil, fmt.Errorf("%w: task %s", ErrTaskNotFound, rawID)
	}
	log := resp.Response.PushLogs[0]
	return tencentCDNBuildTaskStatus(tencentCDNTaskPushPrefix+log.TaskId, log.Status, log.CreateTime, log.UpdateTime), nil
}

func tencentCDNBuildTaskStatus(taskID, status, createTime, updateTime string) *TaskStatus {
	s := tencentCDNMapTaskStatus(status)
	out := &TaskStatus{TaskID: taskID, Status: s}
	if s == TaskStatusDone {
		out.Progress = 100
	}
	if createTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", createTime); err == nil {
			out.CreatedAt = t
		}
	}
	if (s == TaskStatusDone || s == TaskStatusFailed) && updateTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", updateTime); err == nil {
			out.FinishedAt = &t
		}
	}
	return out
}

// ── Unsupported ───────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) GetCacheConfig(_ context.Context, _ string) (*CacheConfig, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) SetCacheConfig(_ context.Context, _ string, _ CacheConfig) error {
	return ErrUnsupported
}
func (p *tencentCDNProvider) GetOriginConfig(_ context.Context, _ string) (*OriginConfig, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) SetOriginConfig(_ context.Context, _ string, _ OriginConfig) error {
	return ErrUnsupported
}
func (p *tencentCDNProvider) GetAccessControl(_ context.Context, _ string) (*AccessControl, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) SetAccessControl(_ context.Context, _ string, _ AccessControl) error {
	return ErrUnsupported
}
func (p *tencentCDNProvider) GetHTTPSConfig(_ context.Context, _ string) (*HTTPSConfig, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) SetHTTPSConfig(_ context.Context, _ string, _ HTTPSConfig) error {
	return ErrUnsupported
}
func (p *tencentCDNProvider) GetPerformanceConfig(_ context.Context, _ string) (*PerformanceConfig, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) SetPerformanceConfig(_ context.Context, _ string, _ PerformanceConfig) error {
	return ErrUnsupported
}
func (p *tencentCDNProvider) GetBandwidthStats(_ context.Context, _ string, _ StatsRequest) ([]BandwidthPoint, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) GetTrafficStats(_ context.Context, _ string, _ StatsRequest) ([]TrafficPoint, error) {
	return nil, ErrUnsupported
}
func (p *tencentCDNProvider) GetHitRateStats(_ context.Context, _ string, _ StatsRequest) ([]HitRatePoint, error) {
	return nil, ErrUnsupported
}

// ── HTTP + signing ────────────────────────────────────────────────────────────

func (p *tencentCDNProvider) call(ctx context.Context, action string, payload any, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("tencent_cdn marshal request: %w", err)
	}

	// Use the real host for signing; in tests baseURL points at httptest.Server
	// and the mock server does not verify the TC3 signature.
	host := tencentCDNHost
	if p.baseURL != tencentCDNBaseURL {
		host = p.baseURL
	}

	headers := p.signer.Headers(host, tencentCDNService, action, tencentCDNVersion, body, p.now())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tencent_cdn build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpResp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("tencent_cdn request: %w", err)
	}
	respBody, _ := io.ReadAll(httpResp.Body)
	httpResp.Body.Close()

	if err := tencentCDNCheckHTTP(httpResp.StatusCode, respBody); err != nil {
		return err
	}
	if dest != nil {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("tencent_cdn parse response: %w", err)
		}
	}
	return nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

func tencentCDNCheckHTTP(code int, body []byte) error {
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
		return fmt.Errorf("tencent_cdn HTTP %d: %s", code, msg)
	}
}

func tencentCDNMapCode(code, message string) error {
	switch code {
	case "AuthFailure", "AuthFailure.InvalidSecretId", "AuthFailure.SignatureFailure",
		"AuthFailure.TokenFailure":
		return fmt.Errorf("%w: %s", ErrUnauthorized, message)
	case "ResourceNotFound.CdnHostNotExists", "ResourceNotFound.CdnDomainNotExists":
		return fmt.Errorf("%w: %s", ErrDomainNotFound, message)
	case "ResourceInUse.CdnHostExists", "ResourceInUse.CdnOpInProgress":
		return fmt.Errorf("%w: %s", ErrDomainAlreadyExists, message)
	case "RequestLimitExceeded":
		return fmt.Errorf("%w: %s", ErrRateLimitExceeded, message)
	default:
		// Surface the error but don't wrap as a sentinel — callers can still check errors.Is.
		err := fmt.Errorf("tencent_cdn API error %s: %s", code, message)
		if strings.Contains(code, "NotFound") {
			return fmt.Errorf("%w: %w", ErrDomainNotFound, err)
		}
		return err
	}
}

