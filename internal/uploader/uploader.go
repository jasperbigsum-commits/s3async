package uploader

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
	"github.com/jasperbigsum-commits/s3async/internal/filter"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type Client struct {
	s3                 *s3.Client
	tm                 *transfermanager.Client
	dryRun             bool
	timeout            time.Duration
	pathStyle          task.PathStyle
	filterRules        []filter.Rule
	multipartThreshold int64
}

// Transfer defaults mirror aws-cli s3 transfer config.
const (
	defaultMultipartThreshold = 8 << 20
	defaultMultipartChunkSize = 8 << 20
	defaultPartConcurrency    = 4
	minUploadPartSize         = 5 << 20
)

// PlanIncrementalUpload compares local files with S3 objects using directional
// modification-time rules and a full-object checksum when the server provides one.
func (c *Client) PlanIncrementalUpload(ctx context.Context, bucket, prefix string, items []task.Item, verifyChecksum ...bool) ([]task.Item, error) {
	if c.s3 == nil || bucket == "" {
		return nil, fmt.Errorf("S3 client and bucket are required to plan incremental upload")
	}
	prefix = strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if prefix != "" {
		prefix += "/"
	}
	remote := make(map[string]types.Object)
	pager := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	for pager.HasMorePages() {
		pageCtx, cancel := context.WithTimeout(ctx, c.timeout)
		page, err := pager.NextPage(pageCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if strings.HasPrefix(key, prefix) {
				remote[strings.TrimPrefix(key, prefix)] = object
			}
		}
	}

	verify := len(verifyChecksum) > 0 && verifyChecksum[0]
	for i := range items {
		relativePath := filepath.ToSlash(items[i].RelativePath)
		remoteKey := relativePath
		if c.pathStyle == task.PathFaithful {
			// items[i].RelativePath is the faithful local form; compare and
			// address by the true S3 suffix instead.
			remoteKey = task.DecodeLocalRelative(relativePath)
		}
		object, ok := remote[remoteKey]
		if !ok || object.Size == nil || object.LastModified == nil || *object.Size != items[i].Size {
			continue
		}
		matches, compareErr := c.localMatchesRemote(ctx, bucket, prefix+remoteKey, items[i].Path, items[i].Size, items[i].ModTime, *object.LastModified, true, true, verify)
		if compareErr != nil {
			return nil, fmt.Errorf("compare local file %s with S3 object: %w", relativePath, compareErr)
		}
		if matches {
			items[i].Status = task.ItemStatusSkipped
		}
	}
	return items, nil
}

// localMatchesRemote applies directional sync comparison after the size has
// matched. Full-object SHA-256 checksums are strongest; multipart composite
// checksums cannot be compared with a whole-file digest and are ignored.
func (c *Client) localMatchesRemote(ctx context.Context, bucket, key, localPath string, expectedSize int64, localModTime, remoteModTime time.Time, exactTimestamps bool, upload bool, verifyChecksum bool) (bool, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != expectedSize || !info.ModTime().Equal(localModTime) {
		// The source changed after planning. Transfer it instead of trusting
		// stale size or timestamp data captured by the initial scan.
		return false, nil
	}
	if verifyChecksum {
		headCtx, cancel := context.WithTimeout(ctx, c.timeout)
		head, headErr := c.s3.HeadObject(headCtx, &s3.HeadObjectInput{Bucket: &bucket, Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
		cancel()
		if headErr == nil {
			if checksum, ok := usableChecksumValue(head.ChecksumSHA256, head.ChecksumType); ok {
				h := sha256.New()
				if _, err := io.Copy(h, file); err != nil {
					return false, fmt.Errorf("hash local file %s: %w", localPath, err)
				}
				return base64.StdEncoding.EncodeToString(h.Sum(nil)) == checksum, nil
			}
			if stored, parseErr := strconv.ParseInt(head.Metadata["s3async-modtime-ns"], 10, 64); parseErr == nil && stored == localModTime.UTC().UnixNano() {
				return true, nil
			}
		}
	}
	if upload {
		return !localModTime.After(remoteModTime), nil
	}
	if !exactTimestamps {
		return true, nil
	}
	return !remoteModTime.After(info.ModTime()), nil
}

func usableChecksumValue(checksum *string, checksumType types.ChecksumType) (string, bool) {
	if checksum == nil || checksumType == types.ChecksumTypeComposite {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(*checksum)
	if err != nil || len(decoded) != sha256.Size {
		return "", false
	}
	return *checksum, true
}

// clientOptions holds the resolved options for building the S3 client
type clientOptions struct {
	region          string
	profile         string
	endpoint        string
	forcePathStyle  bool
	skipTLSVerify   bool
	caCertFile      string
	accessKeyID     string
	secretAccessKey string
}

func buildClientOptions(cfg cfgpkg.Config) clientOptions {
	return clientOptions{
		region:          cfg.S3.Region,
		profile:         cfg.S3.Profile,
		endpoint:        cfg.S3.Endpoint,
		forcePathStyle:  cfg.S3.ForcePathStyle,
		skipTLSVerify:   cfg.S3.SkipTLSVerify,
		caCertFile:      cfg.S3.CACertFile,
		accessKeyID:     cfg.S3.StaticCredentials.AccessKeyID,
		secretAccessKey: cfg.S3.StaticCredentials.SecretAccessKey,
	}
}

func buildHTTPClient(opts clientOptions, timeout time.Duration, maxIdleConnsPerHost int) (*http.Client, error) {
	// Clone the defaults (proxy, keep-alives) instead of mutating the shared
	// transport. Idle pool sizing follows the effective transfer concurrency
	// so part streams reuse keep-alive connections instead of re-handshaking.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected default transport type %T", http.DefaultTransport)
	}
	transport := base.Clone()
	if maxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = maxIdleConnsPerHost
		if transport.MaxIdleConns < maxIdleConnsPerHost*2 {
			transport.MaxIdleConns = maxIdleConnsPerHost * 2
		}
	}
	// Bounds time-to-first-byte per request. Long bodies stream past it:
	// each multipart part is its own request, so large files are unaffected.
	transport.ResponseHeaderTimeout = timeout

	if opts.endpoint != "" {
		if opts.skipTLSVerify {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		} else if opts.caCertFile != "" {
			caCert, err := os.ReadFile(opts.caCertFile)
			if err != nil {
				return nil, fmt.Errorf("read ca cert file: %w", err)
			}
			certPool := x509.NewCertPool()
			certPool.AppendCertsFromPEM(caCert)
			transport.TLSClientConfig = &tls.Config{RootCAs: certPool}
		}
	}

	// Bounds the whole of one HTTP request (a LIST page, a HEAD, a single
	// small object, or one multipart part). Whole-object transfers intentionally
	// carry no total deadline: parts stream for the object's lifetime.
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func buildLoadOptions(ctx context.Context, opts clientOptions) ([]func(*awsconfig.LoadOptions) error, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{}

	if opts.region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(opts.region))
	}

	// Static credentials take precedence over profile
	if opts.accessKeyID != "" && opts.secretAccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.accessKeyID, opts.secretAccessKey, ""),
		))
	} else if opts.profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(opts.profile))
	}

	return loadOptions, nil
}

func New(ctx context.Context, cfg cfgpkg.Config) (*Client, error) {
	timeout := cfg.S3.RequestTimeout
	if timeout < 0 {
		return nil, fmt.Errorf("S3 request timeout must be positive")
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	pathStyle, err := task.ParsePathStyle(cfg.PathStyle)
	if err != nil {
		return nil, err
	}
	// Config file values are strictly validated at load time; normalize here
	// as well so hand-built configs (tests, embeddings) stay on sane ground.
	threshold := cfg.S3.MultipartThreshold
	if threshold <= 0 {
		threshold = defaultMultipartThreshold
	}
	chunkSize := cfg.S3.MultipartChunkSize
	if chunkSize < minUploadPartSize {
		chunkSize = defaultMultipartChunkSize
	}
	partConcurrency := cfg.S3.PartConcurrency
	if partConcurrency <= 0 {
		partConcurrency = defaultPartConcurrency
	}
	if cfg.Security.DryRun {
		return &Client{dryRun: true, timeout: timeout, pathStyle: pathStyle}, nil
	}

	opts := buildClientOptions(cfg)

	idlePerHost := cfg.Workers*partConcurrency + 8
	if idlePerHost < 16 {
		idlePerHost = 16
	}
	httpClient, err := buildHTTPClient(opts, timeout, idlePerHost)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	loadOptions, err := buildLoadOptions(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("build load options: %w", err)
	}
	loadOptions = append(loadOptions, awsconfig.WithHTTPClient(httpClient))

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// Configure S3 options
	var s3Opts []func(*s3.Options)
	if opts.endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = &opts.endpoint
			o.UsePathStyle = opts.forcePathStyle
		})
	}

	s3client := s3.NewFromConfig(awsCfg, s3Opts...)
	tm := transfermanager.New(s3client, func(o *transfermanager.Options) {
		o.PartSizeBytes = chunkSize
		o.MultipartUploadThreshold = threshold
		o.Concurrency = partConcurrency
		// Range retrieval works on every object regardless of how it was
		// uploaded; part-number retrieval requires matching MPU part layouts
		// and is unreliable on S3-compatible stores.
		o.GetObjectType = tmtypes.GetObjectRanges
	})

	return &Client{s3: s3client, tm: tm, dryRun: cfg.Security.DryRun, timeout: timeout, pathStyle: pathStyle, multipartThreshold: threshold}, nil
}

func (c *Client) UploadFile(bucket string, key string, localPath string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}
	if c.dryRun || c.s3 == nil {
		return nil
	}
	if c.tm == nil {
		return fmt.Errorf("transfer manager is required")
	}

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file %s: %w", localPath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat local file %s: %w", localPath, err)
	}
	// No total deadline: large files legitimately stream longer than any
	// single-request timeout. Each underlying request (single PUT or one
	// multipart part) is bounded by the HTTP client timeout instead. The
	// deferred cancel still releases multipart resources on early return.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = c.tm.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   file,
		ACL:    tmtypes.ObjectCannedACLPrivate,
		Metadata: map[string]string{
			"s3async-modtime-ns": strconv.FormatInt(info.ModTime().UTC().UnixNano(), 10),
		},
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}

	return nil
}
