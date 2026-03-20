package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Profile      string
	Region       string
	Bucket       string
	Prefix       string
	Workers      int
	DatabasePath string
	StateDir     string
	Retry        RetryConfig
	Filters      FilterConfig
	Security     SecurityConfig
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

	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Dir(cfg.DatabasePath)
	}

	return cfg, nil
}
