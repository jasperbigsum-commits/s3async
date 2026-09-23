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
	items, err := client.PlanDownload(context.Background(), "bucket", "backup/", root, []string{"*.txt"}, []string{"skip.txt"}, CollisionFail)
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
	items, err := client.PlanIncrementalDownload(context.Background(), "bucket", "backup", root, nil, nil, CollisionFail)
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
			err := client.DownloadFile("bucket", "backup/file.txt", root, "file.txt", 3)
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
	for _, key := range []string{"../escape", "a/../../escape", "a\\b", "C:foo", "NUL.txt", "a.", "a/"} {
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
	if err := client.DownloadFile("bucket", "a", root, "sub/a", 0); err != nil {
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
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionFail)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanRejectsCaseCollisions(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>A.txt</Key><Size>1</Size></Contents><Contents><Key>a.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionFail)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("error=%v", err)
	}
}

func TestLocalPathDotSegments(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"./a.txt", "sub/./a.txt", "./sub/./a.txt"} {
		got, err := LocalPath(root, key)
		if err != nil || got != filepath.Join(root, filepath.FromSlash(key)) {
			t.Fatalf("%q: %q %v", key, got, err)
		}
	}
	for _, key := range []string{".", "./.", "./../escape", "sub/./../../escape"} {
		if _, err := LocalPath(root, key); err == nil {
			t.Fatalf("accepted %q", key)
		}
	}
}

func TestPlanRejectsDotAliasCollision(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a.txt</Key><Size>1</Size></Contents><Contents><Key>./a.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	if _, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionFail); err == nil {
		t.Fatal("accepted alias collision")
	}
}

func TestDownloadPreservesDotKey(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bucket/backup/sub/./a.txt" {
			t.Errorf("changed key: %s", r.URL.Path)
		}
		fmt.Fprint(w, "data")
	})
	root := t.TempDir()
	if err := client.DownloadFile("bucket", "backup/sub/./a.txt", root, "sub/./a.txt", 4); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sub", "a.txt"))
	if err != nil || string(data) != "data" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestSlashFolderDownload(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") != "" {
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup//a.txt</Key><Size>4</Size></Contents></ListBucketResult>`)
			return
		}
		if r.URL.Path != "/bucket/backup//a.txt" {
			t.Errorf("key changed: %q", r.URL.Path)
		}
		fmt.Fprint(w, "data")
	})
	root := t.TempDir()
	items, err := client.PlanDownload(context.Background(), "bucket", "backup/", root, nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RelativePath != "/a.txt" || items[0].Path != filepath.Join(root, "a.txt") {
		t.Fatalf("%+v", items)
	}
	if err := client.DownloadFile("bucket", "backup/"+items[0].RelativePath, root, items[0].RelativePath, items[0].Size); err != nil {
		t.Fatal(err)
	}
}

func TestSlashFolderCollision(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a/b.txt</Key></Contents><Contents><Key>a//b.txt</Key></Contents></ListBucketResult>`)
	})
	if _, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionFail); err == nil {
		t.Fatal("accepted collision")
	}
}

func TestPlanCollisionErrorNamesBothObjects(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>sub/a.txt</Key><Size>1</Size></Contents><Contents><Key>sub/./a.txt</Key><Size>2</Size></Contents></ListBucketResult>`)
	})
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionFail)
	if err == nil {
		t.Fatal("accepted collision")
	}
	for _, want := range []string{"sub/a.txt", "sub/./a.txt", "local file", "--on-collision skip"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestPlanCollisionSkipMarksBothSkipped(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a.txt</Key><Size>1</Size></Contents><Contents><Key>./a.txt</Key><Size>2</Size></Contents><Contents><Key>b.txt</Key><Size>3</Size></Contents></ListBucketResult>`)
	})
	items, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionSkip)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Status != task.ItemStatusSkipped || items[0].Error == "" || !strings.Contains(items[0].Error, "./a.txt") {
		t.Fatalf("first item not marked with collision: %+v", items[0])
	}
	if items[1].Status != task.ItemStatusSkipped || items[1].Error == "" || !strings.Contains(items[1].Error, "a.txt") {
		t.Fatalf("second item not marked with collision: %+v", items[1])
	}
	if items[2].Status != task.ItemStatusPending || items[2].RelativePath != "b.txt" {
		t.Fatalf("unrelated item affected: %+v", items[2])
	}
}

func TestPlanCollisionSkipWithPrefix(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup/a.txt</Key><Size>1</Size></Contents><Contents><Key>backup//a.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	items, err := client.PlanDownload(context.Background(), "bucket", "backup", t.TempDir(), nil, nil, CollisionSkip)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != task.ItemStatusSkipped || items[1].Status != task.ItemStatusSkipped {
		t.Fatalf("items=%+v", items)
	}
	if !strings.Contains(items[0].Error, "backup//a.txt") || !strings.Contains(items[1].Error, "backup/a.txt") {
		t.Fatalf("errors do not name full keys: %q %q", items[0].Error, items[1].Error)
	}
}

func TestPlanCollisionSkipStillFailsFileDirConflict(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a</Key><Size>1</Size></Contents><Contents><Key>a/b</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	_, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionSkip)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanCollisionSkipSkippedPathIgnoresDirCheck(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>a</Key><Size>1</Size></Contents><Contents><Key>./a</Key><Size>1</Size></Contents><Contents><Key>a/b</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	items, err := client.PlanDownload(context.Background(), "bucket", "", t.TempDir(), nil, nil, CollisionSkip)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Status != task.ItemStatusSkipped || items[1].Status != task.ItemStatusSkipped {
		t.Fatalf("colliding items not skipped: %+v", items)
	}
	if items[2].Status != task.ItemStatusPending || items[2].RelativePath != "a/b" {
		t.Fatalf("pending item affected: %+v", items[2])
	}
}

func TestParseCollisionPolicy(t *testing.T) {
	for _, value := range []string{"fail", "FAIL", "", "  skip  ", "SKIP"} {
		policy, err := ParseCollisionPolicy(value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		want := CollisionFail
		if strings.Contains(strings.ToLower(value), "skip") {
			want = CollisionSkip
		}
		if policy != want {
			t.Fatalf("%q: policy=%v want %v", value, policy, want)
		}
	}
	if _, err := ParseCollisionPolicy("overwrite"); err == nil {
		t.Fatal("accepted invalid policy")
	}
}

func faithfulClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	client := testClient(t, handler)
	client.pathStyle = task.PathFaithful
	return client
}

func TestPlanFaithfulKeepsDoubleSlashKeysDistinct(t *testing.T) {
	client := faithfulClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup/a.txt</Key><Size>1</Size></Contents><Contents><Key>backup//a.txt</Key><Size>2</Size></Contents><Contents><Key>backup/./b.txt</Key><Size>3</Size></Contents></ListBucketResult>`)
	})
	root := t.TempDir()
	items, err := client.PlanDownload(context.Background(), "bucket", "backup", root, nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	wantPaths := []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "%2F", "a.txt"),
		filepath.Join(root, "%2E", "b.txt"),
	}
	for i, want := range wantPaths {
		if items[i].Path != want {
			t.Errorf("items[%d].Path = %q, want %q", i, items[i].Path, want)
		}
		if items[i].Status != task.ItemStatusPending {
			t.Errorf("items[%d].Status = %s, want pending", i, items[i].Status)
		}
	}
	if items[0].RelativePath != "a.txt" || items[1].RelativePath != "/a.txt" || items[2].RelativePath != "./b.txt" {
		t.Fatalf("relative paths rewritten: %+v", items)
	}
}

func TestFaithfulDownloadPreservesExactKey(t *testing.T) {
	client := faithfulClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") != "" {
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup//a.txt</Key><Size>4</Size></Contents></ListBucketResult>`)
			return
		}
		if r.URL.Path != "/bucket/backup//a.txt" {
			t.Errorf("key changed: %q", r.URL.Path)
		}
		fmt.Fprint(w, "data")
	})
	root := t.TempDir()
	items, err := client.PlanDownload(context.Background(), "bucket", "backup/", root, nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != filepath.Join(root, "%2F", "a.txt") {
		t.Fatalf("%+v", items)
	}
	if err := client.DownloadFile("bucket", "backup/"+items[0].RelativePath, root, items[0].RelativePath, items[0].Size); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "%2F", "a.txt"))
	if err != nil || string(data) != "data" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestFaithfulDotDotStaysUnderRoot(t *testing.T) {
	collapse := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup/../escape.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	if _, err := collapse.PlanDownload(context.Background(), "bucket", "backup", t.TempDir(), nil, nil, CollisionFail); err == nil {
		t.Fatal("collapse accepted dotdot escape")
	}
	faithful := faithfulClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>backup/../escape.txt</Key><Size>1</Size></Contents></ListBucketResult>`)
	})
	root := t.TempDir()
	items, err := faithful.PlanDownload(context.Background(), "bucket", "backup", root, nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != filepath.Join(root, "%2E%2E", "escape.txt") {
		t.Fatalf("%+v", items)
	}
}

func TestFaithfulSymlinkStillRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := LocalPathWithStyle(root, "link/file", task.PathFaithful); err == nil {
		t.Fatal("faithful accepted symlink")
	}
	// Safety rules other than dot handling are untouched: absolute escape via
	// backslash-style segments is still rejected.
	if _, err := LocalPathWithStyle(root, `a\b`, task.PathFaithful); err == nil {
		t.Fatal("faithful accepted backslash segment")
	}
}

func TestPlanDownloadDedupesRepeatedKey(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>prod/a.jpg</Key><Size>1</Size></Contents><Contents><Key>prod/a.jpg</Key><Size>1</Size></Contents><Contents><Key>prod/b.jpg</Key><Size>2</Size></Contents></ListBucketResult>`)
	})
	items, err := client.PlanDownload(context.Background(), "bucket", "prod", t.TempDir(), nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].RelativePath != "a.jpg" || items[1].RelativePath != "b.jpg" {
		t.Fatalf("items=%+v", items)
	}
}

func TestPlanDownloadDuplicateKeepsLatestState(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>prod/a.jpg</Key><Size>1</Size></Contents><Contents><Key>prod/a.jpg</Key><Size>2</Size></Contents></ListBucketResult>`)
	})
	items, err := client.PlanDownload(context.Background(), "bucket", "prod", t.TempDir(), nil, nil, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Size != 2 {
		t.Fatalf("items=%+v, want single item with latest size 2", items)
	}
}
