package uploader

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3 *s3.Client
}

func New(client *s3.Client) *Client {
	return &Client{s3: client}
}

func (c *Client) UploadPlaceholder(ctx context.Context, bucket string, key string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}

	_ = ctx
	return nil
}
