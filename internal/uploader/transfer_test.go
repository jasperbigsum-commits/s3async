package uploader

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func transferTestClient(t *testing.T, handler http.HandlerFunc, threshold, chunkSize int64, concurrency int) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s3client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), RetryMaxAttempts: 1}, func(o *s3.Options) {
		o.BaseEndpoint = &server.URL
		o.UsePathStyle = true
	})
	tm := transfermanager.New(s3client, func(o *transfermanager.Options) {
		o.PartSizeBytes = chunkSize
		o.MultipartUploadThreshold = threshold
		o.Concurrency = concurrency
		o.GetObjectType = tmtypes.GetObjectRanges
	})
	return &Client{s3: s3client, tm: tm, timeout: 30 * time.Second, multipartThreshold: threshold}
}

func deterministicBytes(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte((i * 31) % 251)
	}
	return buf
}

func TestUploadSmallFileSinglePut(t *testing.T) {
	var mu sync.Mutex
	puts, creates := 0, 0
	var meta, acl string
	client := transferTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPut && !q.Has("partNumber"):
			puts++
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Errorf("read body: %v", err)
			}
			meta = r.Header.Get("X-Amz-Meta-S3async-Modtime-Ns")
			acl = r.Header.Get("X-Amz-Acl")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && q.Has("uploads"):
			creates++
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>small.bin</Key><UploadId>uid-1</UploadId></InitiateMultipartUploadResult>`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}, 8<<20, 8<<20, 4)

	path := filepath.Join(t.TempDir(), "small.bin")
	if err := os.WriteFile(path, deterministicBytes(1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.UploadFile("bucket", "small.bin", path); err != nil {
		t.Fatal(err)
	}
	if puts != 1 || creates != 0 {
		t.Fatalf("puts=%d creates=%d, want single PUT without MPU", puts, creates)
	}
	if meta == "" {
		t.Fatal("modtime metadata missing on single PUT")
	}
	if acl != "private" {
		t.Fatalf("acl=%q, want private", acl)
	}
}

func TestUploadLargeFileMultipart(t *testing.T) {
	const total = 20 << 20
	var mu sync.Mutex
	creates, completes := 0, 0
	var partSizes []int64
	var createMeta string
	client := transferTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			creates++
			createMeta = r.Header.Get("X-Amz-Meta-S3async-Modtime-Ns")
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>big.bin</Key><UploadId>uid-9</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read part: %v", err)
			}
			partSizes = append(partSizes, int64(len(body)))
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%s"`, q.Get("partNumber")))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && q.Has("uploadId"):
			completes++
			body, _ := io.ReadAll(r.Body)
			for _, n := range []string{"1", "2", "3"} {
				if !strings.Contains(string(body), fmt.Sprintf("<PartNumber>%s</PartNumber>", n)) {
					t.Errorf("complete body missing part %s: %s", n, body)
				}
			}
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Location>http://x/bucket/big.bin</Location><Bucket>bucket</Bucket><Key>big.bin</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}, 8<<20, 8<<20, 4)

	path := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(path, deterministicBytes(total), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.UploadFile("bucket", "big.bin", path); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || completes != 1 {
		t.Fatalf("creates=%d completes=%d, want 1 each", creates, completes)
	}
	if len(partSizes) != 3 {
		t.Fatalf("parts=%v, want 3 parts", partSizes)
	}
	sort.Slice(partSizes, func(i, j int) bool { return partSizes[i] > partSizes[j] })
	if partSizes[0] != 8<<20 || partSizes[1] != 8<<20 || partSizes[2] != 4<<20 {
		t.Fatalf("part sizes=%v, want [8MiB 8MiB 4MiB]", partSizes)
	}
	if createMeta == "" {
		t.Fatal("modtime metadata missing on CreateMultipartUpload")
	}
}

func TestDownloadSmallFileSingleGet(t *testing.T) {
	var mu sync.Mutex
	gets, heads := 0, 0
	client := transferTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodHead:
			heads++
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			gets++
			if r.Header.Get("Range") != "" {
				t.Errorf("small file fetched with range %q", r.Header.Get("Range"))
			}
			fmt.Fprint(w, "data")
		}
	}, 8<<20, 8<<20, 4)

	root := t.TempDir()
	if err := client.DownloadFile("bucket", "a.txt", root, "a.txt", 4); err != nil {
		t.Fatal(err)
	}
	if gets != 1 || heads != 0 {
		t.Fatalf("gets=%d heads=%d, want exactly one plain GET", gets, heads)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(data) != "data" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestDownloadLargeFileMultipartAssembly(t *testing.T) {
	const total = 20 << 20
	source := deterministicBytes(total)
	modTime := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	var mu sync.Mutex
	rangeGets := 0
	client := transferTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rangeGets++
		defer mu.Unlock()
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.Itoa(total))
			w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			rangeHeader := r.Header.Get("Range")
			var start, end int
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
				t.Errorf("bad range %q: %v", rangeHeader, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if start < 0 || end >= total || start > end {
				t.Errorf("range out of bounds %q", rangeHeader)
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			rangeGets++
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(source[start : end+1])
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}, 8<<20, 8<<20, 4)

	root := t.TempDir()
	if err := client.DownloadFile("bucket", "big.bin", root, "big.bin", total); err != nil {
		t.Fatal(err)
	}
	if rangeGets < 2 {
		t.Fatalf("rangeGets=%d, want concurrent ranged requests", rangeGets)
	}
	data, err := os.ReadFile(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != total {
		t.Fatalf("len=%d, want %d", len(data), total)
	}
	for i := range data {
		if data[i] != source[i] {
			t.Fatalf("byte %d differs", i)
		}
	}
	info, err := os.Stat(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().UTC().Equal(modTime) {
		t.Fatalf("mtime=%v, want %v", info.ModTime().UTC(), modTime)
	}
}
