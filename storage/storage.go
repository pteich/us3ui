// Package storage defines the protocol-agnostic layer the rest of the app
// works against: object listings, transfers and the sync engine all speak in
// terms of Store. S3 is one implementation; further protocols (FTP, SFTP)
// plug in beside it without the UI knowing which one it is talking to.
package storage

import (
	"context"
	"io"
	"net/url"
	"time"

	"github.com/pteich/us3ui/config"
)

// ObjectInfo describes one remote file. Keys are slash-separated paths
// without a leading slash, so a directory tree can be implied from them.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time

	// ContentType is set by StatObject where the protocol has the concept;
	// listings may leave it empty.
	ContentType string
}

// BucketInfo describes one top-level container (an S3 bucket).
type BucketInfo struct {
	Name string
}

// Capabilities reports the optional features of a connection type, so the
// UI can hide what a protocol cannot do before a connection even exists.
type Capabilities struct {
	Presign bool // presigned share links
	Buckets bool // bucket management
}

// CapsFor returns the capabilities of a connection type
// (see config.S3Config.ConnType).
func CapsFor(connType string) Capabilities {
	switch connType {
	case config.TypeS3:
		return Capabilities{Presign: true, Buckets: true}
	default:
		return Capabilities{}
	}
}

// Store is one remote connection. Implementations must be safe for
// concurrent use.
type Store interface {
	// ListObjectsBatch lists up to batchSize objects with keys ordered
	// lexicographically under prefix, starting after startAfter ("" lists
	// from the beginning).
	ListObjectsBatch(ctx context.Context, startAfter, prefix string, batchSize int) ([]ObjectInfo, error)

	// StatObject returns the metadata of one object.
	StatObject(ctx context.Context, key string) (ObjectInfo, error)

	// UploadObjectReader stores the length bytes of r as objectName.
	// mimeType may be ignored by protocols without a content-type concept.
	UploadObjectReader(ctx context.Context, objectName string, r io.Reader, length int64, mimeType string) error

	// DownloadObject returns a reader of one object's contents; the caller
	// closes it.
	DownloadObject(ctx context.Context, objectName string) (io.ReadCloser, error)

	// DeleteObject removes one object.
	DeleteObject(ctx context.Context, objectName string) error
}

// Presigner is implemented by stores that can hand out temporary share
// links for objects.
type Presigner interface {
	GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (*url.URL, error)
}

// BucketManager is implemented by stores whose top-level containers can be
// listed and managed.
type BucketManager interface {
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	CreateBucket(ctx context.Context, bucketName, region string) error
	DeleteBucket(ctx context.Context, bucketName string) error
}
