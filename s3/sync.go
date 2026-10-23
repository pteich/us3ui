package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/minio/minio-go/v7"
)

const (
	defaultSyncWorkers = 4
	syncListBatchSize  = 1000
)

// SyncAction is a single object that needs copying from source to target.
type SyncAction struct {
	SrcKey      string
	DstKey      string
	Size        int64
	ContentType string
	Reason      string // "missing" | "size differs"
}

// PlanSync lists both sides and returns the objects that must be copied.
// Comparison is size-only: an object is copied when it is missing on the target
// or its byte size differs.
func PlanSync(ctx context.Context, src, dst *Service, srcPrefix, dstPrefix string) ([]SyncAction, error) {
	srcObjs, err := src.listAll(ctx, srcPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing source: %w", err)
	}
	dstObjs, err := dst.listAll(ctx, dstPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing target: %w", err)
	}
	return diffObjects(srcObjs, dstObjs, srcPrefix, dstPrefix), nil
}

// diffObjects is the pure comparison core (no network) so it can be unit tested.
func diffObjects(src, dst []minio.ObjectInfo, srcPrefix, dstPrefix string) []SyncAction {
	dstSize := make(map[string]int64, len(dst))
	for _, o := range dst {
		dstSize[o.Key] = o.Size
	}

	sp := normalizePrefix(srcPrefix)
	dp := normalizePrefix(dstPrefix)

	var actions []SyncAction
	for _, o := range src {
		rel := strings.TrimPrefix(o.Key, sp)
		rel = strings.TrimLeft(rel, "/")
		if rel == "" {
			continue // the prefix placeholder object itself
		}

		dstKey := rel
		if dp != "" {
			dstKey = dp + "/" + rel
		}

		size, ok := dstSize[dstKey]
		var reason string
		switch {
		case !ok:
			reason = "missing"
		case size != o.Size:
			reason = "size differs"
		default:
			continue
		}

		ct := o.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}

		actions = append(actions, SyncAction{
			SrcKey:      o.Key,
			DstKey:      dstKey,
			Size:        o.Size,
			ContentType: ct,
			Reason:      reason,
		})
	}
	return actions
}

// normalizePrefix trims surrounding whitespace and slashes so key joins are predictable.
func normalizePrefix(p string) string {
	return strings.Trim(strings.TrimSpace(p), "/")
}

// RunSync copies the planned actions with a bounded worker pool, streaming each
// object source->target without touching local disk. onProgress reports byte and
// count progress (called frequently, throttle in the callback); onResult fires
// once per object with its outcome. Returns ctx.Err() if cancelled.
func RunSync(ctx context.Context, src, dst *Service, actions []SyncAction, workers int,
	onProgress func(doneBytes, totalBytes int64, doneCount, total int, current string),
	onResult func(a SyncAction, err error)) error {

	if workers <= 0 {
		workers = defaultSyncWorkers
	}

	var totalBytes int64
	for _, a := range actions {
		totalBytes += a.Size
	}
	total := len(actions)

	var doneBytes atomic.Int64
	var doneCount atomic.Int64

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, a := range actions {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(a SyncAction) {
			defer wg.Done()
			defer func() { <-sem }()

			err := copyOne(ctx, src, dst, a, func(n int) {
				nb := doneBytes.Add(int64(n))
				if onProgress != nil {
					onProgress(nb, totalBytes, int(doneCount.Load()), total, a.SrcKey)
				}
			})
			doneCount.Add(1)
			if onResult != nil {
				onResult(a, err)
			}
		}(a)
	}

	wg.Wait()
	return ctx.Err()
}

func copyOne(ctx context.Context, src, dst *Service, a SyncAction, onBytes func(int)) error {
	info, err := src.client.StatObject(ctx, src.bucketName, a.SrcKey, minio.StatObjectOptions{})
	if err != nil {
		return err
	}
	contentType := info.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	obj, err := src.DownloadObject(ctx, a.SrcKey)
	if err != nil {
		return err
	}
	defer obj.Close()

	reader := &countingReader{r: obj, on: onBytes}
	return dst.UploadObjectReader(ctx, a.DstKey, a.DstKey, reader, a.Size, contentType)
}

// listAll pages through every object under prefix using the existing batch lister.
func (s *Service) listAll(ctx context.Context, prefix string) ([]minio.ObjectInfo, error) {
	prefix = strings.TrimLeft(strings.TrimSpace(prefix), "/")

	var all []minio.ObjectInfo
	startAfter := ""
	for {
		batch, err := s.ListObjectsBatch(ctx, startAfter, prefix, syncListBatchSize)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if len(batch) < syncListBatchSize {
			break
		}
		startAfter = batch[len(batch)-1].Key
	}
	return all, nil
}

type countingReader struct {
	r  io.Reader
	on func(n int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.on != nil {
		c.on(n)
	}
	return n, err
}
