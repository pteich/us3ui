package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"

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
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	return &Service{client, cfg.Bucket}, nil
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

func (s *Service) ListObjectsBatch(ctx context.Context, startAfter string, batchSize int) ([]minio.ObjectInfo, error) {
	opts := minio.ListObjectsOptions{
		Prefix:     "",
		Recursive:  true,
		StartAfter: startAfter,
	}

	objectCh := s.client.ListObjects(ctx, s.bucketName, opts)
	var objects []minio.ObjectInfo

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		objects = append(objects, object)
		if len(objects) >= batchSize {
			break
		}
	}

	return objects, nil
}
