package uploader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), RetryMaxAttempts: 1}, func(o *s3.Options) { o.BaseEndpoint = &server.URL; o.UsePathStyle = true })
	return &Client{s3: client, timeout: time.Second * 5}
}

func TestPlanDownloadPaginationAndFilter(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("prefix") != "backup/" {
			t.Errorf("prefix = %q", r.URL.Query().Get("prefix"))
		}
		w.Header().Set("Content-Type", "application/xml")
		if calls == 1 {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>backup/</Key><Size>0</Size></Contents><Contents><Key>backup/sub/a.txt</Key><Size>3</Size></Contents></ListBucketResult>`)
		} else {
			if r.URL.Query().Get("continuation-token") != "next" {
				t.Error("missing continuation token")
			}
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>backup/b.log</Key><Size>2</Size></Contents><Contents><Key>backup/skip.txt</Key><Size>2</Size></Contents></ListBucketResult>`)
		}
	})
	root := t.TempDir()
	items, err := client.PlanDownload(context.Background(), "bucket", "backup/", root, []string{"*.txt"}, []string{"skip.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(items) != 1 || items[0].RelativePath != "sub/a.txt" || items[0].Path != filepath.Join(root, "sub", "a.txt") || items[0].Size != 3 {
		t.Fatalf("calls=%d items=%+v", calls, items)
	}
}

func TestPlanIncrementalDownloadSkipsUnchangedFiles(t *testing.T) {
	modTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListBucketResult><Contents><Key>backup/unchanged.txt</Key><Size>3</Size><LastModified>%s</LastModified></Contents><Contents><Key>backup/changed.txt</Key><Size>4</Size><LastModified>%s</LastModified></Contents></ListBucketResult>`, modTime.Format(time.RFC3339), modTime.Format(time.RFC3339))
	})
	root := t.TempDir()
	unchanged := filepath.Join(root, "unchanged.txt")
	changed := filepath.Join(root, "changed.txt")
	if err := os.WriteFile(unchanged, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unchanged, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	items, err := client.PlanIncrementalDownload(context.Background(), "bucket", "backup", root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != task.ItemStatusSkipped || items[1].Status != task.ItemStatusPending {
		t.Fatalf("items=%+v", items)
	}
}

func TestDownloadReplacementAndFailureCleanup(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/bucket/backup/file.txt" {
					t.Errorf("path=%s", r.URL.Path)
				}
				if broken {
					w.Header().Set("Content-Length", "20")
				}
				fmt.Fprint(w, "new")
			})
			root := t.TempDir()
			local := filepath.Join(root, "file.txt")
			if err := os.WriteFile(local, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			err := client.DownloadFile("bucket", "backup/file.txt", root, "file.txt")
			if (err != nil) != broken {
				t.Fatalf("error=%v broken=%v", err, broken)
			}
			data, _ := os.ReadFile(local)
			want := "new"
			if broken {
				want = "old"
			}
			if string(data) != want {
				t.Fatalf("content=%q", data)
			}
			leftovers, _ := filepath.Glob(filepath.Join(root, ".s3async-*"))
			if len(leftovers) > 0 {
				t.Fatalf("temporary files: %v", leftovers)
			}
		})
	}
}

func TestLocalPathRejectsUnsafeKeysAndSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"../escape", "a/../../escape", "/absolute", "a//b", "a/./b", "a\\b", "C:foo", "NUL.txt", "a.", "a/"} {
		if _, err := LocalPath(root, key); err == nil {
			t.Errorf("accepted %q", key)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := LocalPath(root, "link/file"); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestDownloadDryRunDoesNotCreateDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	client := &Client{dryRun: true}
	if err := client.DownloadFile("bucket", "a", root, "sub/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("destination created: %v", err)
	}
}

func TestPlanRejectsConflictingObjects(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a</Key><Size>1</Size></Contents><Contents><Key>a/b</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanRejectsCaseCollisions(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>A.txt</Key><Size>1</Size></Contents><Contents><Key>a.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("error=%v", err)
	}
}
