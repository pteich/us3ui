package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pteich/us3ui/internal/safepath"
)

// ErrExists reports that a file of the same name already exists at the
// download destination; the local file was left untouched.
var ErrExists = safepath.ErrExists

// ErrObjectChanged reports that the remote object changed after it was
// listed, so its contents were refused instead of downloading a mixture of
// two versions. Refresh the listing and try again.
var ErrObjectChanged = errors.New("object changed since it was listed")

// DownloadToFile downloads the object key into destDir and returns the path
// it was published at. info must come from the same listing the user picked
// the key from; its Size is verified against what was received, and its ETag
// (when the store implements ConditionalDownloader) is sent as a condition,
// so an object rewritten between listing and download fails the transfer
// instead of storing mixed content.
//
// The content is written to a temporary file inside the destination
// directory first and only published under its final name after
// verification, so a crash or failed transfer never leaves a partial or
// truncated file there. A file of the same name is never overwritten:
// DownloadToFile reports storage.ErrExists instead. Progress is reported as
// the number of bytes received so far (onProgress may be nil).
func DownloadToFile(ctx context.Context, svc Store, key string, info ObjectInfo, destDir string, onProgress func(written int64)) (string, error) {
	final, err := safepath.Join(destDir, key)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(final), ".us3ui-download-*")
	if err != nil {
		return "", err
	}
	published := false
	defer func() {
		if !published {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	var rc io.ReadCloser
	if info.ETag != "" {
		if cd, ok := svc.(ConditionalDownloader); ok {
			rc, err = cd.DownloadObjectIfMatch(ctx, key, info.ETag)
		} else {
			rc, err = svc.DownloadObject(ctx, key)
		}
	} else {
		rc, err = svc.DownloadObject(ctx, key)
	}
	if err != nil {
		return "", err
	}

	var src io.Reader = rc
	if onProgress != nil {
		var received int64
		src = &countingReader{r: rc, on: func(n int) {
			received += int64(n)
			onProgress(received)
		}}
	}
	written, err := io.Copy(tmp, src)
	closeErr := rc.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = tmp.Sync()
	}
	if err != nil {
		return "", err
	}
	if info.Size >= 0 && written != info.Size {
		return "", fmt.Errorf("received %d bytes but the object is %d bytes (a proxy may have altered the content)", written, info.Size)
	}

	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := safepath.Publish(tmp.Name(), final); err != nil {
		return "", err
	}
	published = true
	return final, nil
}
