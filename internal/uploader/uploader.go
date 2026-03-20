package uploader

import (
	"context"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
)

type Client struct {
	s3     *s3.Client
	dryRun bool
}

func New(ctx context.Context, cfg cfgpkg.Config) (*Client, error) {
	if cfg.Security.DryRun || cfg.Bucket == "" {
		return &Client{dryRun: true}, nil
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &Client{s3: s3.NewFromConfig(awsCfg), dryRun: false}, nil
}

func (c *Client) UploadFile(ctx context.Context, bucket string, key string, localPath string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}
	if c.dryRun || c.s3 == nil {
		return nil
	}

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
