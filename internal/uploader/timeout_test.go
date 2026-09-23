package uploader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
)

func TestConfiguredDownloadTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
			fmt.Fprint(w, "data")
		}
	}))
	defer server.Close()
	for _, timeout := range []time.Duration{30 * time.Millisecond, 3 * time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			cfg := cfgpkg.Config{S3: cfgpkg.S3Config{
				Region: "us-east-1", Endpoint: server.URL, ForcePathStyle: true, RequestTimeout: timeout,
				StaticCredentials: cfgpkg.StaticCredentialsConfig{AccessKeyID: "test", SecretAccessKey: "test"},
			}}
			client, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			local := filepath.Join(root, "a.txt")
			if err := os.WriteFile(local, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			err = client.DownloadFile("bucket", "a.txt", root, "a.txt", 4)
			want := "data"
			if timeout < time.Second {
				want = "old"
				if err == nil {
					t.Fatal("expected timeout while reading body")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(local)
			if err != nil || string(data) != want {
				t.Fatalf("content=%q err=%v", data, err)
			}
			leftovers, err := filepath.Glob(filepath.Join(root, ".s3async-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files=%v err=%v", leftovers, err)
			}
		})
	}
}
