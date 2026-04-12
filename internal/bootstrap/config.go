package bootstrap

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the top-level application configuration.
// All fields are populated from configs/config.yaml and/or environment variables.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	DB       DBConfig       `mapstructure:"db"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Storage  StorageConfig  `mapstructure:"storage"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Agent    AgentConfig    `mapstructure:"agent"`
	Telegram TelegramConfig `mapstructure:"telegram"`
	Webhook  WebhookConfig  `mapstructure:"webhook"`
}

type ServerConfig struct {
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	AgentPort   int    `mapstructure:"agent_port"`   // mTLS listener for Pull Agent protocol
	TLSCertFile string `mapstructure:"tls_cert_file"` // server cert (signed by Agent CA)
	TLSKeyFile  string `mapstructure:"tls_key_file"`
	CACertFile  string `mapstructure:"ca_cert_file"` // Agent CA — used to verify agent client certs
}

type DBConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Name     string `mapstructure:"name"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"ssl_mode"`
	MaxOpen  int    `mapstructure:"max_open"`
	MaxIdle  int    `mapstructure:"max_idle"`
}

// DSN returns a libpq-style connection string for sqlx.
func (c DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.Port, c.Name, c.User, c.Password, c.SSLMode,
	)
}

// URL returns a postgres:// URL for golang-migrate.
func (c DBConfig) URL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Name, c.SSLMode,
	)
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// StorageConfig configures the S3-compatible object store (MinIO in dev, AWS S3 in prod).
type StorageConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	Bucket          string `mapstructure:"bucket"`
	AccessKey       string `mapstructure:"access_key"`
	SecretKey       string `mapstructure:"secret_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	ArtifactsBucket string `mapstructure:"artifacts_bucket"`
}

type JWTConfig struct {
	Secret string        `mapstructure:"secret"`
	Expiry time.Duration `mapstructure:"expiry"`
}

// AgentConfig holds mTLS certificate paths and runtime settings for cmd/agent.
type AgentConfig struct {
	ControlPlaneURL string `mapstructure:"control_plane_url"`
	CertFile        string `mapstructure:"cert_file"` // agent client cert
	KeyFile         string `mapstructure:"key_file"`
	CACertFile      string `mapstructure:"ca_cert_file"` // Agent CA cert (to verify server)
	HeartbeatSecs   int    `mapstructure:"heartbeat_secs"`
	DeployPath      string `mapstructure:"deploy_path"`  // default: /var/www
	NginxPath       string `mapstructure:"nginx_path"`   // default: /etc/nginx/conf.d
	StagingPath     string `mapstructure:"staging_path"`  // default: /tmp/agent-staging
	SigningKey      string `mapstructure:"signing_key"`   // HMAC secret for signature verification
	AllowReload     bool   `mapstructure:"allow_reload"`  // whether nginx reload is permitted
	HostGroup       string `mapstructure:"host_group"`    // agent host group tag
	Region          string `mapstructure:"region"`        // agent region tag
}

type TelegramConfig struct {
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`
}

type WebhookConfig struct {
	URL string `mapstructure:"url"`
}

// Load reads the config file and binds environment variables.
// Config file location: ./configs/config.yaml (relative to working directory).
// Environment variables override file values via Viper's AutomaticEnv.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./configs")
	v.AddConfigPath(".")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.agent_port", 8443)
	v.SetDefault("db.host", "localhost")
	v.SetDefault("db.port", 5432)
	v.SetDefault("db.ssl_mode", "disable")
	v.SetDefault("db.max_open", 25)
	v.SetDefault("db.max_idle", 5)
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("jwt.expiry", "24h")
	v.SetDefault("storage.use_ssl", false)
	v.SetDefault("storage.bucket", "artifacts")
	v.SetDefault("storage.artifacts_bucket", "artifacts")
	v.SetDefault("agent.heartbeat_secs", 30)
	v.SetDefault("agent.deploy_path", "/var/www")
	v.SetDefault("agent.nginx_path", "/etc/nginx/conf.d")
	v.SetDefault("agent.staging_path", "/tmp/agent-staging")
	v.SetDefault("agent.allow_reload", true)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// config file missing is OK — rely on defaults + env vars
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}
