package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/pteich/us3ui/config"
)

type Service struct {
	client     *minio.Client
	bucketName string
}

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

func (s *Service) ListObjects(ctx context.Context) ([]minio.ObjectInfo, error) {
	objectCh := s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{
		Recursive: true,
	})
	var objects []minio.ObjectInfo
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		objects = append(objects, obj)
	}
	return objects, nil
}

func (s *Service) DeleteObject(ctx context.Context, objectName string) error {
	return s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
}

func (s *Service) UploadObjectReader(ctx context.Context, filePath string, objectName string, r io.Reader, length int64, mimeType string) error {
	_, err := s.client.PutObject(ctx, s.bucketName, objectName,
		r,
		length,
		minio.PutObjectOptions{
			ContentType: mimeType,
		})
	return err
}

func (s *Service) UploadObject(filePath string, data []byte) error {
	ctx := context.Background()
	objectName := filepath.Base(filePath)

	_, err := s.client.PutObject(ctx, s.bucketName, objectName,
		bytes.NewReader(data),
		int64(len(data)),
		minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
	return err
}

func (s *Service) DownloadObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucketName, objectName, minio.GetObjectOptions{})
}

func (s *Service) GetPresignedURL(ctx context.Context, objectName string, expires time.Duration) (*url.URL, error) {
	return s.client.PresignedGetObject(ctx, s.bucketName, objectName, expires, nil)
}

func (s *Service) ListObjectsBatch(ctx context.Context, startAfter, prefix string, batchSize int) ([]minio.ObjectInfo, error) {
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
	objects := make([]minio.ObjectInfo, 0, batchSize)

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		objects = append(objects, object)
		if len(objects) >= batchSize {
			cancel()
			break
		}
	}

	return objects, nil
}

func (s *Service) ListBuckets(ctx context.Context) ([]minio.BucketInfo, error) {
	return s.client.ListBuckets(ctx)
}

func (s *Service) CreateBucket(ctx context.Context, bucketName string, region string) error {
	return s.client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: region})
}

func (s *Service) DeleteBucket(ctx context.Context, bucketName string) error {
	return s.client.RemoveBucket(ctx, bucketName)
}
