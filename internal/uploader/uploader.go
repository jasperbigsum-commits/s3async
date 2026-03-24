package uploader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
)

type Client struct {
	s3      *s3.Client
	dryRun  bool
	timeout time.Duration
}

// clientOptions holds the resolved options for building the S3 client
type clientOptions struct {
	region         string
	profile        string
	endpoint       string
	forcePathStyle bool
	skipTLSVerify  bool
	caCertFile     string
	accessKeyID    string
	secretAccessKey string
}

func buildClientOptions(cfg cfgpkg.Config) clientOptions {
	return clientOptions{
		region:         cfg.S3.Region,
		profile:        cfg.S3.Profile,
		endpoint:       cfg.S3.Endpoint,
		forcePathStyle: cfg.S3.ForcePathStyle,
		skipTLSVerify:  cfg.S3.SkipTLSVerify,
		caCertFile:     cfg.S3.CACertFile,
		accessKeyID:    cfg.S3.StaticCredentials.AccessKeyID,
		secretAccessKey: cfg.S3.StaticCredentials.SecretAccessKey,
	}
}

func buildHTTPClient(opts clientOptions) (*http.Client, error) {
	transport := http.DefaultTransport

	if opts.endpoint != "" {
		if opts.skipTLSVerify {
			transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		} else if opts.caCertFile != "" {
			caCert, err := os.ReadFile(opts.caCertFile)
			if err != nil {
				return nil, fmt.Errorf("read ca cert file: %w", err)
			}
			certPool := x509.NewCertPool()
			certPool.AppendCertsFromPEM(caCert)
			transport = &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: certPool},
			}
		}
	}

	return &http.Client{Transport: transport}, nil
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
	// Use S3 config (already normalized from legacy fields)
	s3Cfg := cfg.S3
	if s3Cfg.Bucket == "" {
		s3Cfg.Bucket = cfg.Bucket
	}

	if cfg.Security.DryRun || s3Cfg.Bucket == "" {
		return &Client{dryRun: true, timeout: 30 * time.Second}, nil
	}

	opts := buildClientOptions(cfg)

	httpClient, err := buildHTTPClient(opts)
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

	return &Client{s3: s3.NewFromConfig(awsCfg, s3Opts...), dryRun: false, timeout: 30 * time.Second}, nil
}

func (c *Client) UploadFile(bucket string, key string, localPath string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}
	if c.dryRun || c.s3 == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file %s: %w", localPath, err)
	}
	defer file.Close()

	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   file,
		ACL:    types.ObjectCannedACLPrivate,
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}

	return nil
}
