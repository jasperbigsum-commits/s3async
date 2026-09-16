package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, env string
		want            time.Duration
		invalid         bool
	}{
		{"default", "", "", 30 * time.Second, false},
		{"file", "s3:\n  request_timeout: 30m\n", "", 30 * time.Minute, false},
		{"environment", "s3:\n  request_timeout: 30m\n", "2h", 2 * time.Hour, false},
		{"zero", "", "0s", 0, true},
		{"negative", "", "-1s", 0, true},
		{"unitless", "", "30", 0, true},
		{"invalid", "", "bad", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("S3ASYNC_S3_REQUEST_TIMEOUT", tc.env)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v", err)
			}
			if !tc.invalid && cfg.S3.RequestTimeout != tc.want {
				t.Fatalf("timeout=%v want %v", cfg.S3.RequestTimeout, tc.want)
			}
		})
	}
}
