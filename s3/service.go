package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Service struct {
	client     *minio.Client
	bucketName string
}

func New(endpoint, accessKey, secretKey, bucketName string, useSSL bool) (*Service, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	return &Service{client, bucketName}, nil
}

func (s *Service) ListObjects() ([]minio.ObjectInfo, error) {
	ctx := context.Background()
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

func (s *Service) DeleteObject(objectName string) error {
	ctx := context.Background()
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

func (s *Service) DownloadObject(objectName string) (io.ReadCloser, error) {
	return s.client.GetObject(context.Background(), s.bucketName, objectName, minio.GetObjectOptions{})
}
