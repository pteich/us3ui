package storage

import (
	"context"
	"crypto/rand"
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

// DownloadConflict is the user's choice when a local download target exists.
type DownloadConflict int

const (
	SkipDownload DownloadConflict = iota
	ReplaceDownload
	KeepBothDownloads
)

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
// truncated file there. onConflict resolves existing targets before any
// network transfer, and any conflicts appearing during the transfer without
// downloading again. A nil callback skips conflicts with ErrExists.
// onProgress reports bytes received and may be nil.
func DownloadToFile(ctx context.Context, svc Store, key string, info ObjectInfo, destDir string, onProgress func(written int64), onConflict func(path string) (DownloadConflict, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name, err := safepath.Join(root, key)
	if err != nil {
		return "", err
	}
	// Pin the parent so directory changes cannot separate the temp and target.
	parent, err := root.OpenRoot(filepath.Dir(name))
	if err != nil {
		return "", err
	}
	defer parent.Close()
	final := filepath.Base(name)
	choose := func() (DownloadConflict, error) {
		if err := ctx.Err(); err != nil {
			return SkipDownload, err
		}
		if onConflict == nil {
			return SkipDownload, ErrExists
		}
		choice, err := onConflict(filepath.Join(destDir, name))
		if err != nil {
			return SkipDownload, err
		}
		if err := ctx.Err(); err != nil {
			return SkipDownload, err
		}
		switch choice {
		case SkipDownload:
			return choice, ErrExists
		case ReplaceDownload, KeepBothDownloads:
			return choice, nil
		default:
			return choice, fmt.Errorf("invalid download conflict choice %d", choice)
		}
	}
	choice := SkipDownload
	if _, err := parent.Lstat(final); err == nil {
		choice, err = choose()
		if err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	tempName := ".us3ui-download-" + rand.Text()
	tmp, err := parent.OpenFile(tempName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer func() {
		tmp.Close()
		parent.Remove(tempName)
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
		err = ctx.Err()
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
	for n := 1; ; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		target := final
		if choice == KeepBothDownloads {
			ext := filepath.Ext(final)
			target = fmt.Sprintf("%s (%d)%s", final[:len(final)-len(ext)], n, ext)
			n++
		}
		err := safepath.Publish(parent, tempName, target, choice == ReplaceDownload)
		if !errors.Is(err, ErrExists) {
			if err != nil {
				return "", err
			}
			return filepath.Join(destDir, filepath.Dir(name), target), nil
		}
		if choice != KeepBothDownloads {
			choice, err = choose()
			if err != nil {
				return "", err
			}
		}
	}
}
