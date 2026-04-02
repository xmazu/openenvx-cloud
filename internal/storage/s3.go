package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ContentType  string
}

type Storage struct {
	client *s3.Client
	bucket string
}

type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
}

func NewStorage(cfg Config) (*Storage, error) {
	scheme := "https"
	if !cfg.UseSSL {
		scheme = "http"
	}

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		BaseEndpoint: aws.String(fmt.Sprintf("%s://%s", scheme, cfg.Endpoint)),
	})

	return &Storage{
		client: client,
		bucket: cfg.BucketName,
	}, nil
}

func (s *Storage) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		// Bucket doesn't exist, create it
		_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(s.bucket),
		})
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
	}
	return nil
}

func (s *Storage) Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectName),
		Body:        reader,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload object %s: %w", objectName, err)
	}

	return objectName, nil
}

func (s *Storage) GetPresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)

	presignedReq, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectName),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("generate presigned URL: %w", err)
	}

	return presignedReq.URL, nil
}

func (s *Storage) Download(ctx context.Context, objectName string) (io.ReadCloser, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		return nil, fmt.Errorf("download object %s: %w", objectName, err)
	}
	return resp.Body, nil
}

func (s *Storage) Stat(ctx context.Context, objectName string) (ObjectInfo, error) {
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat object %s: %w", objectName, err)
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	var lastModified time.Time
	if resp.LastModified != nil {
		lastModified = *resp.LastModified
	}

	var contentType string
	if resp.ContentType != nil {
		contentType = *resp.ContentType
	}

	return ObjectInfo{
		Key:          objectName,
		Size:         size,
		LastModified: lastModified,
		ContentType:  contentType,
	}, nil
}
