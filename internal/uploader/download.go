package uploader

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jasperbigsum-commits/s3async/internal/filter"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

// LocalPath rejects keys that cannot be mapped unambiguously to local files.
// Existing symlinks below the destination are never followed.
func LocalPath(root, relative string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("destination must be absolute")
	}
	if relative == "" || strings.ContainsAny(relative, "\\\x00:") {
		return "", fmt.Errorf("unsafe object path %q", relative)
	}
	current := root
	parts := strings.Split(relative, "/")
	for i, part := range append([]string{""}, parts...) {
		if i > 0 {
			if part == "" || part == "." || part == ".." || strings.TrimRight(part, " .") != part {
				return "", fmt.Errorf("unsafe object path %q", relative)
			}
			base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
			if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
				return "", fmt.Errorf("reserved object path %q", relative)
			}
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink in destination: %s", current)
		}
		if i < len(parts) && !info.IsDir() {
			return "", fmt.Errorf("destination parent is not a directory: %s", current)
		}
		if i == len(parts) && !info.Mode().IsRegular() {
			return "", fmt.Errorf("destination is not a regular file: %s", current)
		}
	}
	return current, nil
}

func (c *Client) PlanDownload(ctx context.Context, bucket, prefix, root string, include, exclude []string) ([]task.Item, error) {
	if c.s3 == nil || bucket == "" {
		return nil, fmt.Errorf("S3 client and bucket are required to list objects")
	}
	prefix = strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if prefix != "" {
		prefix += "/"
	}
	pager := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	var items []task.Item
	for pager.HasMorePages() {
		pageCtx, cancel := context.WithTimeout(ctx, c.timeout)
		page, err := pager.NextPage(pageCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if !strings.HasPrefix(key, prefix) {
				return nil, fmt.Errorf("object outside requested prefix: %q", key)
			}
			if strings.HasSuffix(key, "/") {
				continue
			}
			relative := strings.TrimPrefix(key, prefix)
			if !filter.Match(relative, include, exclude) {
				continue
			}
			local, err := LocalPath(root, relative)
			if err != nil {
				return nil, err
			}
			items = append(items, task.Item{Path: local, RelativePath: relative, Size: aws.ToInt64(object.Size), Status: task.ItemStatusPending})
		}
	}
	// Reject file/directory conflicts before starting any downloads.
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		// Use a portable comparison so a plan is safe on case-insensitive filesystems.
		name := strings.ToLower(item.RelativePath)
		if paths[name] {
			return nil, fmt.Errorf("object path collision: %q", item.RelativePath)
		}
		paths[name] = true
	}
	for _, item := range items {
		for parent := filepath.ToSlash(filepath.Dir(item.RelativePath)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if paths[strings.ToLower(parent)] {
				return nil, fmt.Errorf("object file/directory conflict: %q", parent)
			}
		}
	}
	return items, nil
}

func (c *Client) DownloadFile(bucket, key, root, relative string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}
	local, err := LocalPath(root, relative)
	if err != nil {
		return err
	}
	if c.dryRun {
		return nil
	}
	if c.s3 == nil {
		return fmt.Errorf("S3 client is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	response, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("get object %s: %w", key, err)
	}
	defer response.Body.Close()
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return err
	}
	if _, err := LocalPath(root, relative); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(local), ".s3async-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	size, err := io.Copy(temp, response.Body)
	if err != nil {
		return fmt.Errorf("download %s: %w", key, err)
	}
	if response.ContentLength != nil && size != *response.ContentLength {
		return fmt.Errorf("incomplete download %s: got %d bytes, expected %d", key, size, *response.ContentLength)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if response.LastModified != nil {
		if err := os.Chtimes(temp.Name(), *response.LastModified, *response.LastModified); err != nil {
			return err
		}
	}
	if _, err := LocalPath(root, relative); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), local); err != nil {
		return fmt.Errorf("replace local file: %w", err)
	}
	return nil
}
