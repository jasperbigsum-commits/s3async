package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/task"
	"github.com/spf13/viper"
)

type Config struct {
	Profile      string
	Region       string
	Bucket       string
	Prefix       string
	Workers      int
	PathStyle    string
	DatabasePath string
	StateDir     string
	Retry        RetryConfig
	Filters      FilterConfig
	Security     SecurityConfig
	S3           S3Config
}

type S3Config struct {
	RequestTimeout    time.Duration
	Profile           string
	Region            string
	Bucket            string
	Prefix            string
	Endpoint          string
	ForcePathStyle    bool
	SkipTLSVerify     bool
	CACertFile        string
	MultipartThreshold int64
	MultipartChunkSize int64
	PartConcurrency    int
	StaticCredentials StaticCredentialsConfig
}

type StaticCredentialsConfig struct {
	AccessKeyID     string
	SecretAccessKey string
}

type RetryConfig struct {
	MaxAttempts int
	BackoffMS   int
}

type FilterConfig struct {
	Include []string
	Exclude []string
}

type SecurityConfig struct {
	RedactLogs bool
	DryRun     bool
}

func Load(configPath string) (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user home dir: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("S3ASYNC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("profile", "default")
	v.SetDefault("region", "us-east-1")
	v.SetDefault("bucket", "")
	v.SetDefault("prefix", "")
	v.SetDefault("workers", 4)
	v.SetDefault("path_style", "collapse")
	v.SetDefault("s3.request_timeout", "30s")
	v.SetDefault("s3.multipart_threshold", "8MB")
	v.SetDefault("s3.multipart_chunksize", "8MB")
	v.SetDefault("s3.part_concurrency", 4)
	v.SetDefault("database_path", filepath.Join(homeDir, ".s3async", "tasks.db"))
	v.SetDefault("state_dir", filepath.Join(homeDir, ".s3async"))
	v.SetDefault("retry.max_attempts", 3)
	v.SetDefault("retry.backoff_ms", 500)
	v.SetDefault("filters.include", []string{})
	v.SetDefault("filters.exclude", []string{})
	v.SetDefault("security.redact_logs", true)
	v.SetDefault("security.dry_run", false)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath(filepath.Join(homeDir, ".s3async"))
	}

	if err := v.ReadInConfig(); err != nil {
		if configPath != "" {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
	}

	cfg := Config{
		Profile:      v.GetString("profile"),
		Region:       v.GetString("region"),
		Bucket:       v.GetString("bucket"),
		Prefix:       v.GetString("prefix"),
		Workers:      v.GetInt("workers"),
		PathStyle:    v.GetString("path_style"),
		DatabasePath: v.GetString("database_path"),
		StateDir:     v.GetString("state_dir"),
		Retry: RetryConfig{
			MaxAttempts: v.GetInt("retry.max_attempts"),
			BackoffMS:   v.GetInt("retry.backoff_ms"),
		},
		Filters: FilterConfig{
			Include: v.GetStringSlice("filters.include"),
			Exclude: v.GetStringSlice("filters.exclude"),
		},
		Security: SecurityConfig{
			RedactLogs: v.GetBool("security.redact_logs"),
			DryRun:     v.GetBool("security.dry_run"),
		},
	}

	// Resolve S3Config: new s3.* takes precedence over legacy fields
	s3Cfg, err := resolveS3Config(v, cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.S3 = s3Cfg

	// Sync legacy fields from resolved S3 config for backward compatibility
	if cfg.S3.Profile != "" {
		cfg.Profile = cfg.S3.Profile
	}
	if cfg.S3.Region != "" {
		cfg.Region = cfg.S3.Region
	}
	if cfg.S3.Bucket != "" {
		cfg.Bucket = cfg.S3.Bucket
	}
	if cfg.S3.Prefix != "" {
		cfg.Prefix = cfg.S3.Prefix
	}

	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	// The path style must agree between planning and execution (including the
	// daemon, which reloads this file), so it is validated up front.
	style, err := task.ParsePathStyle(cfg.PathStyle)
	if err != nil {
		return Config{}, err
	}
	cfg.PathStyle = style.String()
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Dir(cfg.DatabasePath)
	}

	return cfg, nil
}

func resolveS3Config(v *viper.Viper, legacy Config) (S3Config, error) {
	s3Cfg := S3Config{
		Profile:        v.GetString("s3.profile"),
		Region:         v.GetString("s3.region"),
		Bucket:         v.GetString("s3.bucket"),
		Prefix:         v.GetString("s3.prefix"),
		Endpoint:       v.GetString("s3.endpoint"),
		ForcePathStyle: v.GetBool("s3.force_path_style"),
		SkipTLSVerify:  v.GetBool("s3.skip_tls_verify"),
		CACertFile:     v.GetString("s3.ca_cert_file"),
		StaticCredentials: StaticCredentialsConfig{
			AccessKeyID:     v.GetString("s3.static_credentials.access_key_id"),
			SecretAccessKey: v.GetString("s3.static_credentials.secret_access_key"),
		},
	}

	timeoutText := v.GetString("s3.request_timeout")
	if timeoutText == "" {
		timeoutText = "30s"
	}
	timeout, err := time.ParseDuration(timeoutText)
	if err != nil || timeout <= 0 {
		return S3Config{}, fmt.Errorf("s3.request_timeout must be a positive duration with units (e.g. 30s, 30m, 2h)")
	}
	s3Cfg.RequestTimeout = timeout

	thresholdText := v.GetString("s3.multipart_threshold")
	if thresholdText == "" {
		thresholdText = "8MB"
	}
	threshold, err := parseByteSize(thresholdText)
	if err != nil || threshold <= 0 {
		return S3Config{}, fmt.Errorf("s3.multipart_threshold must be a positive byte size (e.g. 8388608, 8MB, 1GB)")
	}
	s3Cfg.MultipartThreshold = threshold

	chunkText := v.GetString("s3.multipart_chunksize")
	if chunkText == "" {
		chunkText = "8MB"
	}
	chunkSize, err := parseByteSize(chunkText)
	if err != nil || chunkSize < 5*1024*1024 {
		return S3Config{}, fmt.Errorf("s3.multipart_chunksize must be a byte size of at least 5MB (e.g. 8MB, 16MB)")
	}
	s3Cfg.MultipartChunkSize = chunkSize

	concurrency := v.GetInt("s3.part_concurrency")
	if concurrency == 0 {
		concurrency = 4
	}
	if concurrency < 1 {
		return S3Config{}, fmt.Errorf("s3.part_concurrency must be at least 1")
	}
	s3Cfg.PartConcurrency = concurrency

	// Apply defaults from legacy if not set in s3.*
	if s3Cfg.Profile == "" {
		s3Cfg.Profile = legacy.Profile
	}
	if s3Cfg.Region == "" {
		s3Cfg.Region = legacy.Region
	}
	if s3Cfg.Bucket == "" {
		s3Cfg.Bucket = legacy.Bucket
	}
	if s3Cfg.Prefix == "" {
		s3Cfg.Prefix = legacy.Prefix
	}

	// Validate static_credentials: must have both or neither
	hasAccessKey := s3Cfg.StaticCredentials.AccessKeyID != ""
	hasSecretKey := s3Cfg.StaticCredentials.SecretAccessKey != ""
	if hasAccessKey != hasSecretKey {
		return S3Config{}, fmt.Errorf("static_credentials: must have both access_key_id and secret_access_key, or neither")
	}

	// Validate endpoint format if provided
	if s3Cfg.Endpoint != "" {
		if !strings.HasPrefix(s3Cfg.Endpoint, "http://") && !strings.HasPrefix(s3Cfg.Endpoint, "https://") {
			return S3Config{}, fmt.Errorf("endpoint must be a valid URL with http:// or https:// prefix")
		}
		// For HTTPS endpoints, validate CA cert file if specified
		if strings.HasPrefix(s3Cfg.Endpoint, "https://") && s3Cfg.CACertFile != "" {
			if _, err := os.Stat(s3Cfg.CACertFile); err != nil {
				return S3Config{}, fmt.Errorf("ca_cert_file: %w", err)
			}
		}
	}

	return s3Cfg, nil
}

// parseByteSize parses human-readable byte sizes in aws-cli style: a plain
// integer means bytes, otherwise an integer or decimal number with a
// case-insensitive B/KB/MB/GB/TB suffix (1024-based, e.g. "8MB", "1.5GB").
func parseByteSize(value string) (int64, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, fmt.Errorf("empty byte size")
	}
	i := 0
	for i < len(text) && (text[i] >= '0' && text[i] <= '9' || text[i] == '.') {
		i++
	}
	number, suffix := text[:i], strings.ToUpper(strings.TrimSpace(text[i:]))
	if number == "" {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	amount, err := strconv.ParseFloat(number, 64)
	if err != nil || amount < 0 {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	var multiplier float64 = 1
	switch suffix {
	case "", "B":
		multiplier = 1
	case "K", "KB", "KIB":
		multiplier = 1 << 10
	case "M", "MB", "MIB":
		multiplier = 1 << 20
	case "G", "GB", "GIB":
		multiplier = 1 << 30
	case "T", "TB", "TIB":
		multiplier = 1 << 40
	default:
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	return int64(amount * multiplier), nil
}
