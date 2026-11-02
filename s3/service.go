package s3

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/storage"
)

// Service is the S3 implementation of storage.Store.
type Service struct {
	client     *minio.Client
	bucketName string
}

var (
	_ storage.Store         = (*Service)(nil)
	_ storage.Presigner     = (*Service)(nil)
	_ storage.BucketManager = (*Service)(nil)
)

func New(cfg config.S3Config) (*Service, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Region: cfg.Region,
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	return &Service{client: client, bucketName: cfg.Bucket}, nil
}

func (s *Service) DeleteObject(ctx context.Context, objectName string) error {
	return s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
}

func (s *Service) UploadObjectReader(ctx context.Context, objectName string, r io.Reader, length int64, mimeType string) error {
	_, err := s.client.PutObject(ctx, s.bucketName, objectName,
		r,
		length,
		minio.PutObjectOptions{
			ContentType: mimeType,
		})
	return err
}

func (s *Service) DownloadObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucketName, objectName, minio.GetObjectOptions{})
}

func (s *Service) GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (*url.URL, error) {
	return s.client.PresignedGetObject(ctx, s.bucketName, objectName, expires, nil)
}

func (s *Service) ListObjectsBatch(ctx context.Context, startAfter, prefix string, batchSize int) ([]storage.ObjectInfo, error) {
	opts := minio.ListObjectsOptions{
		WithVersions: false,
		WithMetadata: false,
		MaxKeys:      batchSize,
		Prefix:       prefix,
		Recursive:    true,
		StartAfter:   startAfter,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	objectCh := s.client.ListObjects(ctx, s.bucketName, opts)
	objects := make([]storage.ObjectInfo, 0, batchSize)

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		objects = append(objects, objectInfo(object))
		if len(objects) >= batchSize {
			cancel()
			break
		}
	}

	return objects, nil
}

func (s *Service) StatObject(ctx context.Context, objectName string) (storage.ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return objectInfo(info), nil
}

// objectInfo maps a MinIO object to the protocol-agnostic type.
func objectInfo(o minio.ObjectInfo) storage.ObjectInfo {
	return storage.ObjectInfo{
		Key:          o.Key,
		Size:         o.Size,
		LastModified: o.LastModified,
		ContentType:  o.ContentType,
	}
}

func (s *Service) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	buckets, err := s.client.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.BucketInfo, len(buckets))
	for i, b := range buckets {
		out[i] = storage.BucketInfo{Name: b.Name}
	}
	return out, nil
}

func (s *Service) CreateBucket(ctx context.Context, bucketName string, region string) error {
	return s.client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: region})
}

func (s *Service) DeleteBucket(ctx context.Context, bucketName string) error {
	return s.client.RemoveBucket(ctx, bucketName)
}
