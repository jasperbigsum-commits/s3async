package uploader

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jasperbigsum-commits/s3async/internal/filter"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

// LocalPath rejects keys that cannot be mapped unambiguously to local files.
// Existing symlinks below the destination are never followed.
func LocalPath(root, relative string) (string, error) {
	return LocalPathWithStyle(root, relative, task.PathCollapse)
}

// LocalPathWithStyle maps one S3 relative key under root. In faithful mode
// empty and dot-only segments are escaped reversibly (see task.EncodeKeySegment)
// instead of being dropped, so distinct keys never share a local file; every
// other safety rule (traversal, symlinks, reserved names) still applies.
func LocalPathWithStyle(root, relative string, style task.PathStyle) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("destination must be absolute")
	}
	if relative == "" || strings.HasSuffix(relative, "/") || strings.ContainsAny(relative, "\\\x00:") {
		return "", fmt.Errorf("unsafe object path %q", relative)
	}
	current := root
	rawParts := strings.Split(relative, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if style == task.PathFaithful {
			parts = append(parts, task.EncodeKeySegment(part))
			continue
		}
		if part == "." || part == "" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("object path has no filename: %q", relative)
	}
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

func (c *Client) PlanDownload(ctx context.Context, bucket, prefix, root string, include, exclude []string, policy CollisionPolicy, exactTimestamps ...bool) ([]task.Item, error) {
	exact := len(exactTimestamps) > 0 && exactTimestamps[0]
	return c.planDownload(ctx, bucket, prefix, root, include, exclude, false, policy, exact, false, time.Time{}, time.Time{})
}

func (c *Client) PlanDownloadRange(ctx context.Context, bucket, prefix, root string, include, exclude []string, policy CollisionPolicy, from, to time.Time) ([]task.Item, error) {
	return c.planDownload(ctx, bucket, prefix, root, include, exclude, false, policy, false, false, from, to)
}

// PlanIncrementalDownload applies AWS CLI style size comparison plus optional
// directional timestamp comparison; matching full-object checksums take priority.
func (c *Client) PlanIncrementalDownload(ctx context.Context, bucket, prefix, root string, include, exclude []string, policy CollisionPolicy, exactTimestamps ...bool) ([]task.Item, error) {
	exact := len(exactTimestamps) > 0 && exactTimestamps[0]
	return c.planDownload(ctx, bucket, prefix, root, include, exclude, true, policy, exact, false, time.Time{}, time.Time{})
}

func (c *Client) PlanIncrementalDownloadRange(ctx context.Context, bucket, prefix, root string, include, exclude []string, policy CollisionPolicy, exactTimestamps bool, from, to time.Time, verifyChecksum ...bool) ([]task.Item, error) {
	verify := len(verifyChecksum) > 0 && verifyChecksum[0]
	return c.planDownload(ctx, bucket, prefix, root, include, exclude, true, policy, exactTimestamps, verify, from, to)
}

// CollisionPolicy controls how PlanDownload handles S3 objects that normalize
// to the same local file (for example keys that differ only by redundant
// slashes, dot segments, or letter case).
type CollisionPolicy int

const (
	// CollisionFail aborts the whole plan on the first collision.
	CollisionFail CollisionPolicy = iota
	// CollisionSkip leaves every colliding object out of the download as a
	// skipped item carrying the reason, so the rest of the plan proceeds.
	// Skipped objects never overwrite each other or anything else.
	CollisionSkip
)

// SetFilterRules installs ordered include/exclude rules for the next planning
// call. The client is created per sync command, so the rules are not shared.
func (c *Client) SetFilterRules(rules []filter.Rule) {
	c.filterRules = append([]filter.Rule(nil), rules...)
}

// ParseCollisionPolicy parses the --on-collision flag value.
func ParseCollisionPolicy(value string) (CollisionPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "fail":
		return CollisionFail, nil
	case "skip":
		return CollisionSkip, nil
	default:
		return CollisionFail, fmt.Errorf("invalid collision policy %q: want fail or skip", value)
	}
}

func (p CollisionPolicy) String() string {
	if p == CollisionSkip {
		return "skip"
	}
	return "fail"
}

func (c *Client) planDownload(ctx context.Context, bucket, prefix, root string, include, exclude []string, incremental bool, policy CollisionPolicy, exactTimestamps, verifyChecksum bool, from, to time.Time) ([]task.Item, error) {
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
			matched := filter.Match(relative, include, exclude)
			if c.filterRules != nil {
				matched = filter.MatchWithRules(relative, include, c.filterRules)
			}
			if !matched {
				continue
			}
			if (!from.IsZero() || !to.IsZero()) && object.LastModified == nil {
				continue
			}
			if object.LastModified != nil && ((!from.IsZero() && object.LastModified.Before(from)) || (!to.IsZero() && object.LastModified.After(to))) {
				continue
			}
			local, err := LocalPathWithStyle(root, relative, c.pathStyle)
			if err != nil {
				return nil, err
			}
			item := task.Item{Path: local, RelativePath: relative, Size: aws.ToInt64(object.Size), Status: task.ItemStatusPending}
			if object.LastModified != nil {
				item.ModTime = object.LastModified.UTC()
			}
			if incremental && object.LastModified != nil {
				if info, statErr := os.Stat(local); statErr == nil && info.Mode().IsRegular() && info.Size() == item.Size {
					unchanged, compareErr := c.localMatchesRemote(ctx, bucket, key, local, item.Size, info.ModTime(), *object.LastModified, exactTimestamps, false, verifyChecksum)
					if compareErr != nil {
						return nil, fmt.Errorf("compare local file %s with S3 object: %w", relative, compareErr)
					}
					if unchanged {
						item.Status = task.ItemStatusSkipped
					}
				}
			}
			items = append(items, item)
		}
	}
	// S3 may repeat the exact same key across pages when the bucket is
	// mutated during a multi-page listing (documented LIST behavior under
	// concurrent writes; also seen on some S3-compatible stores). Identical
	// keys name one object, so download it once: later pages reflect newer
	// bucket state and win over earlier sightings.
	byKey := make(map[string]int, len(items))
	deduped := make([]task.Item, 0, len(items))
	for _, item := range items {
		if idx, ok := byKey[prefix+item.RelativePath]; ok {
			deduped[idx] = item
			continue
		}
		byKey[prefix+item.RelativePath] = len(deduped)
		deduped = append(deduped, item)
	}
	items = deduped
	// Reject file/directory conflicts before starting any downloads.
	// Items that share a normalized local path are either fatal (fail policy)
	// or recorded as skipped with the reason (skip policy) so the rest of the
	// plan can proceed without any ambiguous overwrite.
	type seenPath struct {
		index    int
		relative string
	}
	paths := make(map[string]seenPath, len(items))
	for i := range items {
		// Use a portable comparison so a plan is safe on case-insensitive filesystems.
		name := strings.ToLower(filepath.Clean(items[i].Path))
		first, ok := paths[name]
		if !ok {
			paths[name] = seenPath{index: i, relative: items[i].RelativePath}
			continue
		}
		if policy == CollisionSkip {
			markCollisionSkipped(items, prefix, first.index, items[i].RelativePath)
			markCollisionSkipped(items, prefix, i, first.relative)
			continue
		}
		return nil, fmt.Errorf("object path collision: S3 objects %q and %q both map to local file %q; exclude one of them with --exclude, or rerun with --on-collision skip to download everything else",
			prefix+first.relative, prefix+items[i].RelativePath, items[i].Path)
	}
	for i := range items {
		// Skipped objects are never downloaded, so only pending items can
		// create real file/directory conflicts at download time. Per-item
		// conflicts with files that already exist locally are already
		// rejected by LocalPath above.
		if policy == CollisionSkip && items[i].Status == task.ItemStatusSkipped {
			continue
		}
		for parent := filepath.Dir(items[i].Path); parent != root && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
			first, ok := paths[strings.ToLower(parent)]
			if !ok {
				continue
			}
			// Collision-skipped objects are never downloaded, so they cannot
			// create directories at runtime. (Conflicts with files that
			// already exist locally are already rejected per item by
			// LocalPath above.)
			if policy == CollisionSkip && items[first.index].Status == task.ItemStatusSkipped && items[first.index].Error != "" {
				continue
			}
			return nil, fmt.Errorf("object file/directory conflict: S3 object %q needs parent directory %q, which collides with S3 object %q",
				prefix+items[i].RelativePath, parent, prefix+first.relative)
		}
	}
	return items, nil
}

// markCollisionSkipped records a path collision on items[index] without
// touching an already-recorded reason (for example an incremental skip).
func markCollisionSkipped(items []task.Item, prefix string, index int, otherRelative string) {
	items[index].Status = task.ItemStatusSkipped
	if items[index].Error != "" {
		return
	}
	items[index].Error = fmt.Sprintf("object path collision with %q: both map to local file %q", prefix+otherRelative, items[index].Path)
}

func (c *Client) DownloadFile(bucket, key, root, relative string, size int64) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key are required")
	}
	local, err := LocalPathWithStyle(root, relative, c.pathStyle)
	if err != nil {
		return err
	}
	if c.dryRun {
		return nil
	}
	if c.s3 == nil {
		return fmt.Errorf("S3 client is required")
	}
	// Small objects stay on one ranged GET: the transfer manager would spend
	// an extra HEAD per file just to learn the size we already know.
	if c.tm == nil || size < c.multipartThreshold {
		return c.downloadSinglePart(bucket, key, root, relative, local)
	}
	return c.downloadMultipart(bucket, key, root, relative, local)
}

func (c *Client) downloadSinglePart(bucket, key, root, relative, local string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	response, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("get object %s: %w", key, err)
	}
	defer response.Body.Close()
	return c.finishDownload(bucket, key, root, relative, local, response.Body, response.ContentLength, response.LastModified)
}

func (c *Client) downloadMultipart(bucket, key, root, relative, local string) error {
	// No total deadline: parts stream for the whole object lifetime, each
	// bounded by the HTTP client timeout. The deferred cancel still releases
	// background part fetchers if we return before consuming the stream.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := c.tm.GetObject(ctx, &transfermanager.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("get object %s: %w", key, err)
	}
	return c.finishDownload(bucket, key, root, relative, local, out.Body, out.ContentLength, out.LastModified)
}

func (c *Client) finishDownload(bucket, key, root, relative, local string, body io.Reader, contentLength *int64, lastModified *time.Time) error {
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return err
	}
	if _, err := LocalPathWithStyle(root, relative, c.pathStyle); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(local), ".s3async-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	size, err := io.Copy(temp, body)
	if err != nil {
		return fmt.Errorf("download %s: %w", key, err)
	}
	if contentLength != nil && size != *contentLength {
		return fmt.Errorf("incomplete download %s: got %d bytes, expected %d", key, size, *contentLength)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if lastModified != nil {
		if err := os.Chtimes(temp.Name(), *lastModified, *lastModified); err != nil {
			return err
		}
	}
	if _, err := LocalPathWithStyle(root, relative, c.pathStyle); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), local); err != nil {
		return fmt.Errorf("replace local file: %w", err)
	}
	return nil
}
