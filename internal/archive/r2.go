package archive

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ObjectStore is the minimal blob-store surface the archiver needs. Keeping it
// an interface means R2 can be swapped for any S3-compatible target (or a fake
// in tests) without touching the archive logic — part of the no-lock-in goal.
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
}

// R2Store writes objects to Cloudflare R2 via its S3-compatible API.
type R2Store struct {
	client *s3.Client
	bucket string
}

func NewR2Store(ctx context.Context, endpoint, accessKey, secret, bucket string) (*R2Store, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secret, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		// R2 historically rejected the SDK's default trailing checksums; only
		// send a checksum when the operation actually requires one.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &R2Store{client: client, bucket: bucket}, nil
}

func (s *R2Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	return err
}
