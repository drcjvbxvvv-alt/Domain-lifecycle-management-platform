// Package cdn defines the CDN provider abstraction layer.
//
// A Provider represents a CDN service (Aliyun CDN, Tencent Cloud CDN,
// Cloudflare, Huawei Cloud CDN, etc.) that manages domain acceleration,
// cache configuration, access control, HTTPS, performance optimization,
// content purge/prefetch, and traffic statistics.
//
// Each provider implementation reads its config and credentials from the
// cdn_providers table's config/credentials JSONB columns. The factory
// pattern (see registry below) ensures zero changes to internal/ code
// when a new provider is added.
package cdn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrProviderNotRegistered = errors.New("cdn provider type not registered")
	ErrMissingCredentials    = errors.New("cdn provider credentials missing or invalid")
	ErrMissingConfig         = errors.New("cdn provider config missing or invalid")
	ErrDomainNotFound        = errors.New("cdn domain not found")
	ErrDomainAlreadyExists   = errors.New("cdn domain already exists")
	ErrDomainNotReady        = errors.New("cdn domain not in a ready state for this operation")
	ErrTaskNotFound          = errors.New("cdn task not found")
	ErrRateLimitExceeded     = errors.New("cdn provider API rate limit exceeded")
	ErrUnauthorized          = errors.New("cdn provider API credentials rejected")
	ErrUnsupported           = errors.New("operation not supported by this cdn provider")
)

// ── Domain status constants ───────────────────────────────────────────────────

// CDN domain status values returned by GetDomain / ListDomains.
const (
	DomainStatusOnline       = "online"
	DomainStatusOffline      = "offline"
	DomainStatusConfiguring  = "configuring"
	DomainStatusChecking     = "checking"
)

// CDN domain business types.
const (
	BusinessTypeWeb      = "web"
	BusinessTypeDownload = "download"
	BusinessTypeMedia    = "media"
)

// Access control list types.
const (
	ACLTypeWhitelist = "whitelist"
	ACLTypeBlacklist = "blacklist"
)

// CDN task status values.
const (
	TaskStatusPending    = "pending"
	TaskStatusProcessing = "processing"
	TaskStatusDone       = "done"
	TaskStatusFailed     = "failed"
)

// URL authentication types (unified across providers).
const (
	URLAuthTypeA = "TypeA" // timestamp + md5 hash
	URLAuthTypeB = "TypeB" // path-based signing
	URLAuthTypeC = "TypeC" // path + timestamp-based
)

// ── Shared types ──────────────────────────────────────────────────────────────

// HTTPHeader represents a single HTTP header key-value pair.
type HTTPHeader struct {
	Key   string
	Value string
}

// CDNDomain is the CDN platform's representation of an acceleration domain.
type CDNDomain struct {
	Domain       string     // the acceleration domain (e.g. "cdn.example.com")
	CNAME        string     // the CNAME the user must point their DNS record at
	Status       string     // DomainStatus* constant
	BusinessType string     // BusinessType* constant
	CreatedAt    *time.Time
}

// AddDomainRequest is the input for Provider.AddDomain.
type AddDomainRequest struct {
	Domain       string   // acceleration domain to create
	BusinessType string   // BusinessType* constant; defaults to web
	Origins      []Origin // at least one origin required
}

// ── Origin config ─────────────────────────────────────────────────────────────

// OriginConfig describes how a CDN domain pulls content from origin servers.
type OriginConfig struct {
	Origins        []Origin
	OriginProtocol string       // "http" | "https" | "follow" (follow the request)
	OriginHost     string       // override Host header sent to origin; empty = use acceleration domain
	OriginHeaders  []HTTPHeader // extra headers added to origin requests
	Follow302      bool         // whether the CDN should follow 302 redirects from origin
	OriginTimeout  int          // seconds before the CDN gives up waiting for origin
}

// Origin represents a single origin server entry in OriginConfig.
type Origin struct {
	Address string // IP address or domain name
	Port    int    // 0 = use protocol default (80/443)
	Weight  int    // load-balancing weight 1–100; 0 = equal weight
	Type    string // "primary" | "backup"
}

// ── Cache config ──────────────────────────────────────────────────────────────

// CacheConfig controls how the CDN caches responses for a domain.
type CacheConfig struct {
	Rules       []CacheRule
	IgnoreQuery bool             // when true, URL query strings do not affect cache key
	IgnoreCase  bool             // when true, URL paths are case-insensitive for cache key
	StatusCode  []StatusCodeCache
}

// CacheRule defines caching behaviour for a set of URLs matching a pattern.
type CacheRule struct {
	RuleType string // "all" | "suffix" | "directory" | "url" | "regex"
	Pattern  string // e.g. "*.jpg", "/static/", "/api/*"
	TTL      int    // seconds; 0 = no cache; -1 = follow origin Cache-Control
	Priority int    // lower value = higher priority; providers vary
}

// StatusCodeCache overrides the default cache TTL for a specific HTTP status code.
type StatusCodeCache struct {
	Code int // HTTP status code (e.g. 404, 302)
	TTL  int // seconds; 0 = no cache
}

// ── Access control ────────────────────────────────────────────────────────────

// AccessControl bundles all access restriction policies for a CDN domain.
// Each sub-config is optional (nil = feature disabled / not configured).
type AccessControl struct {
	Referer   *RefererControl
	IP        *IPControl
	URLAuth   *URLAuth
	GeoBlock  *GeoBlock
	RateLimit *RateLimit
	UserAgent *UserAgentControl
}

// RefererControl restricts access based on the HTTP Referer header.
type RefererControl struct {
	Type       string   // ACLType* constant
	Domains    []string // domain patterns, e.g. ["example.com", "*.partner.com"]
	AllowEmpty bool     // whether requests with no Referer header are allowed
}

// IPControl restricts access based on client IP address.
type IPControl struct {
	Type string   // ACLType* constant
	IPs  []string // IPv4/IPv6 addresses and CIDR ranges (e.g. "192.168.0.0/24")
}

// URLAuth enables token-based URL signing to prevent hotlinking.
type URLAuth struct {
	Enabled       bool
	Type          string // URLAuthType* constant
	Key           string // signing key (secret)
	ExpireSeconds int    // token validity window in seconds
}

// GeoBlock restricts access by geographic region.
type GeoBlock struct {
	Type    string   // ACLType* constant
	Regions []string // ISO 3166-1 alpha-2 country codes; providers may also accept sub-national codes
}

// RateLimit controls per-IP or per-connection request rate.
type RateLimit struct {
	Enabled   bool
	Threshold int // requests per second before limiting kicks in
	BurstSize int // number of requests allowed to burst above Threshold
}

// UserAgentControl restricts access based on the User-Agent header.
type UserAgentControl struct {
	Type       string   // ACLType* constant
	UserAgents []string // exact strings or wildcard patterns
}

// ── HTTPS config ──────────────────────────────────────────────────────────────

// HTTPSConfig controls TLS settings for a CDN domain.
type HTTPSConfig struct {
	Enabled          bool
	CertID           string      // provider-side certificate ID (from the CDN's cert management)
	ForceHTTPS       bool        // redirect HTTP → HTTPS
	HTTP2            bool
	HTTP3            bool        // QUIC / HTTP/3
	HSTS             *HSTSConfig
	OCSPStapling     bool
	TLSVersions      []string    // enabled TLS versions, e.g. ["TLSv1.2", "TLSv1.3"]
}

// HSTSConfig configures HTTP Strict Transport Security.
type HSTSConfig struct {
	Enabled           bool
	MaxAge            int  // HSTS max-age in seconds
	IncludeSubdomains bool
	Preload           bool
}

// ── Performance config ────────────────────────────────────────────────────────

// PerformanceConfig controls edge-side optimization features.
type PerformanceConfig struct {
	Gzip         bool     // enable gzip compression at edge
	Brotli       bool     // enable brotli compression (higher ratio than gzip)
	FilterParams []string // URL query params that are stripped from cache key
	PageOptimize bool     // minify/merge HTML, CSS, JS (provider support varies)
	RangeOrigin  bool     // enable Range requests to origin (byte-range / resumable downloads)
	VideoSeek    bool     // enable FLV/MP4 video seek (time-based drag for media domains)
}

// ── Statistics ────────────────────────────────────────────────────────────────

// StatsRequest is the time-range + granularity input for statistics queries.
type StatsRequest struct {
	StartTime time.Time
	EndTime   time.Time
	Interval  string // "5min" | "1hour" | "1day"
}

// BandwidthPoint is one data point in a bandwidth time series (bits per second).
type BandwidthPoint struct {
	Time time.Time
	Bps  int64
}

// TrafficPoint is one data point in a traffic time series (total bytes).
type TrafficPoint struct {
	Time  time.Time
	Bytes int64
}

// HitRatePoint is one data point in a cache hit-rate time series (0.0–1.0).
type HitRatePoint struct {
	Time time.Time
	Rate float64
}

// ── Content tasks ─────────────────────────────────────────────────────────────

// PurgeTask represents a cache-invalidation job submitted to a CDN provider.
type PurgeTask struct {
	TaskID    string
	Status    string   // TaskStatus* constant
	URLs      []string // URLs or directory paths that are being purged
	CreatedAt time.Time
}

// PrefetchTask represents a cache-warming job submitted to a CDN provider.
type PrefetchTask struct {
	TaskID    string
	Status    string
	URLs      []string
	CreatedAt time.Time
}

// TaskStatus is the current state of a PurgeTask or PrefetchTask.
type TaskStatus struct {
	TaskID     string
	Status     string     // TaskStatus* constant
	Progress   int        // completion percentage 0–100
	CreatedAt  time.Time
	FinishedAt *time.Time // nil while still in progress
}

// ── Provider interface ────────────────────────────────────────────────────────

// Provider is the abstraction for a CDN hosting provider's management API.
//
// Implementations must be safe for concurrent use. All methods accept a
// context for cancellation and timeout control.
//
// The domain parameter is always the acceleration domain name (e.g.
// "cdn.example.com"), not a provider-specific zone ID. Implementations are
// responsible for any provider-side ID lookups or caching.
//
// Methods that are not supported by a particular provider must return
// ErrUnsupported rather than silently succeeding or panicking.
type Provider interface {
	// Name returns the provider type identifier (e.g. "aliyun", "tencentcloud").
	Name() string

	// ── Domain lifecycle ─────────────────────────────────────────────────────

	// AddDomain creates an acceleration domain on the CDN platform.
	// Returns ErrDomainAlreadyExists if the domain is already present.
	AddDomain(ctx context.Context, req AddDomainRequest) (*CDNDomain, error)

	// RemoveDomain deletes an acceleration domain from the CDN platform.
	// Returns ErrDomainNotFound if the domain does not exist.
	RemoveDomain(ctx context.Context, domain string) error

	// GetDomain returns the current state and metadata of an acceleration domain.
	// Returns ErrDomainNotFound if the domain does not exist.
	GetDomain(ctx context.Context, domain string) (*CDNDomain, error)

	// ListDomains returns all acceleration domains in the provider account.
	ListDomains(ctx context.Context) ([]CDNDomain, error)

	// ── Configuration management ─────────────────────────────────────────────

	// GetCacheConfig retrieves the current cache configuration for a domain.
	GetCacheConfig(ctx context.Context, domain string) (*CacheConfig, error)

	// SetCacheConfig replaces the cache configuration for a domain.
	SetCacheConfig(ctx context.Context, domain string, cfg CacheConfig) error

	// GetOriginConfig retrieves the current origin pull configuration for a domain.
	GetOriginConfig(ctx context.Context, domain string) (*OriginConfig, error)

	// SetOriginConfig replaces the origin pull configuration for a domain.
	SetOriginConfig(ctx context.Context, domain string, cfg OriginConfig) error

	// GetAccessControl retrieves the current access-control configuration.
	GetAccessControl(ctx context.Context, domain string) (*AccessControl, error)

	// SetAccessControl replaces the access-control configuration for a domain.
	SetAccessControl(ctx context.Context, domain string, ac AccessControl) error

	// GetHTTPSConfig retrieves the current HTTPS/TLS configuration for a domain.
	GetHTTPSConfig(ctx context.Context, domain string) (*HTTPSConfig, error)

	// SetHTTPSConfig replaces the HTTPS/TLS configuration for a domain.
	SetHTTPSConfig(ctx context.Context, domain string, cfg HTTPSConfig) error

	// GetPerformanceConfig retrieves the current performance-optimization config.
	GetPerformanceConfig(ctx context.Context, domain string) (*PerformanceConfig, error)

	// SetPerformanceConfig replaces the performance-optimization config for a domain.
	SetPerformanceConfig(ctx context.Context, domain string, cfg PerformanceConfig) error

	// ── Content management ───────────────────────────────────────────────────

	// PurgeURLs submits a cache-invalidation job for the given URLs.
	// Returns the task handle immediately; poll GetTaskStatus for completion.
	PurgeURLs(ctx context.Context, urls []string) (*PurgeTask, error)

	// PurgeDirectory submits a cache-invalidation job for all URLs under dir.
	// dir must end with "/" (e.g. "/static/images/").
	PurgeDirectory(ctx context.Context, dir string) (*PurgeTask, error)

	// PrefetchURLs submits a cache-warming job for the given URLs.
	PrefetchURLs(ctx context.Context, urls []string) (*PrefetchTask, error)

	// GetTaskStatus returns the current state of a purge or prefetch task.
	// Returns ErrTaskNotFound if no task with taskID exists.
	GetTaskStatus(ctx context.Context, taskID string) (*TaskStatus, error)

	// ── Statistics ───────────────────────────────────────────────────────────

	// GetBandwidthStats returns edge-to-client bandwidth (bps) over a time range.
	GetBandwidthStats(ctx context.Context, domain string, req StatsRequest) ([]BandwidthPoint, error)

	// GetTrafficStats returns edge-to-client traffic volume (bytes) over a time range.
	GetTrafficStats(ctx context.Context, domain string, req StatsRequest) ([]TrafficPoint, error)

	// GetHitRateStats returns cache hit rate (0.0–1.0) over a time range.
	GetHitRateStats(ctx context.Context, domain string, req StatsRequest) ([]HitRatePoint, error)
}

// ── Factory ───────────────────────────────────────────────────────────────────

// Factory creates a Provider instance from config and credentials JSON.
// The shape of each JSON blob is provider-specific; see each implementation's
// godoc for the expected fields.
type Factory func(config, credentials json.RawMessage) (Provider, error)

// ── Registry ──────────────────────────────────────────────────────────────────

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds a provider factory to the global registry.
// Implementations call this in their init() function so that importing the
// package is sufficient to make the provider available.
//
// Panics if providerType is empty or if the same type is registered twice,
// because this always indicates a programming mistake.
func Register(providerType string, factory Factory) {
	if providerType == "" {
		panic("cdn: Register called with empty providerType")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[providerType]; dup {
		panic("cdn: provider type already registered: " + providerType)
	}
	registry[providerType] = factory
}

// Get creates a Provider instance for the given type, using the provided
// config and credentials JSON. Returns ErrProviderNotRegistered if the
// type has no registered factory.
func Get(providerType string, config, credentials json.RawMessage) (Provider, error) {
	registryMu.RLock()
	factory, ok := registry[providerType]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotRegistered, providerType)
	}
	return factory(config, credentials)
}

// RegisteredTypes returns a snapshot of all currently registered provider
// type identifiers.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
