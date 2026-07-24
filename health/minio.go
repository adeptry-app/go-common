package health

import (
	"context"
	"time"

	"github.com/minio/minio-go/v7"
)

// MinIOChecker checks MinIO/S3 connection status
type MinIOChecker struct {
	client *minio.Client
	bucket string
}

// NewMinIOChecker creates a new MinIO health checker
// If bucket is provided, it checks if the bucket exists
// If bucket is empty, it lists buckets to verify connectivity
func NewMinIOChecker(client *minio.Client, bucket string) Checker {
	return &MinIOChecker{
		client: client,
		bucket: bucket,
	}
}

// Name returns the name of this checker
func (c *MinIOChecker) Name() string {
	return "minio"
}

// Check verifies the MinIO connection
func (c *MinIOChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	if c.client == nil {
		return Unhealthy(start, "client is nil")
	}

	if c.bucket != "" {
		// Check if specific bucket exists
		exists, err := c.client.BucketExists(ctx, c.bucket)
		if err != nil {
			return Unhealthy(start, "bucket check failed: %v", err)
		}
		if !exists {
			return Degraded(start, "bucket %q does not exist", c.bucket)
		}
	} else {
		// Just verify connectivity by listing buckets
		_, err := c.client.ListBuckets(ctx)
		if err != nil {
			return Unhealthy(start, "list buckets failed: %v", err)
		}
	}

	return Healthy(start)
}
