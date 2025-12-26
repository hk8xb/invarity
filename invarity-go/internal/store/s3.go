// Package store provides data access layer for storage backends.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client wraps the AWS S3 client for canonical JSON operations.
type S3Client struct {
	client *s3.Client
	bucket string
}

// NewS3Client creates a new S3 client wrapper.
func NewS3Client(client *s3.Client, bucket string) *S3Client {
	return &S3Client{
		client: client,
		bucket: bucket,
	}
}

// PutJSON writes a value as canonical JSON to S3.
func (c *S3Client) PutJSON(ctx context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to put object to S3: %w", err)
	}

	return nil
}

// GetJSON reads a value from S3 and unmarshals it.
func (c *S3Client) GetJSON(ctx context.Context, key string, v any) error {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return fmt.Errorf("failed to read S3 object body: %w", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// GetRaw reads raw bytes from S3.
func (c *S3Client) GetRaw(ctx context.Context, key string) ([]byte, error) {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object body: %w", err)
	}

	return data, nil
}

// PutRaw writes raw bytes to S3.
func (c *S3Client) PutRaw(ctx context.Context, key string, data []byte, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("failed to put object to S3: %w", err)
	}

	return nil
}

// Exists checks if a key exists in S3.
func (c *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a "not found" error
		// Note: aws-sdk-go-v2 returns error for 404
		return false, nil
	}
	return true, nil
}

// ToolManifestKey returns the S3 key for a tool manifest.
func ToolManifestKey(tenantID, toolID, version string) string {
	return fmt.Sprintf("manifests/%s/tools/%s/%s.json", tenantID, toolID, version)
}

// ToolsetManifestKey returns the S3 key for a toolset manifest.
func ToolsetManifestKey(tenantID, toolsetID, revision string) string {
	return fmt.Sprintf("manifests/%s/toolsets/%s/%s.json", tenantID, toolsetID, revision)
}
