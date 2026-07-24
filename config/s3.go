package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

// S3Config holds S3/MinIO storage configuration
// AccessKey and SecretKey are optional - if not provided, IAM role credentials will be used (AWS only)
type S3Config struct {
	Endpoint         string `validate:"required,url"`
	AccessKey        string // Optional: required for MinIO, empty for AWS IAM role auth
	SecretKey        string // Optional: required for MinIO, empty for AWS IAM role auth
	UseSSL           bool
	ImagesBucket     string // Bucket for portfolio images
	DocumentsBucket  string // Bucket for documents (PDF, Word)
	MiniaturesBucket string // Bucket for miniature/thumbnail images
	AvatarsBucket    string // Bucket for hero/character avatar images
}

// Env vars whose names appear in both the loader and its diagnostics.
const (
	envS3AccessKey = "S3_ACCESS_KEY"
	envS3SecretKey = "S3_SECRET_KEY" // #nosec G101 -- env var name, not a credential
)

// NewS3Config loads S3 configuration from environment variables
func NewS3Config() S3Config {
	cfg := S3Config{
		Endpoint:         GetEnvRequired("S3_ENDPOINT"),
		AccessKey:        GetEnv(envS3AccessKey, ""), // Optional for IAM role authentication
		SecretKey:        GetEnv(envS3SecretKey, ""), // Optional for IAM role authentication
		UseSSL:           GetEnvBool("S3_USE_SSL", false),
		ImagesBucket:     GetEnv("S3_IMAGES_BUCKET", "images"),               // Default for local MinIO
		DocumentsBucket:  GetEnv("S3_DOCUMENTS_BUCKET", "documents"),         // Default for local MinIO
		MiniaturesBucket: GetEnv("S3_MINIATURES_BUCKET", "miniatures"),       // Default for local MinIO
		AvatarsBucket:    GetEnv("S3_AVATARS_BUCKET", "character-portraits"), // Default for local MinIO
	}

	// Validate endpoint is a valid URL
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid S3 configuration: %v", err))
	}

	// Validate that credentials are provided as a pair (both or neither)
	hasAccessKey := cfg.AccessKey != ""
	hasSecretKey := cfg.SecretKey != ""
	if hasAccessKey != hasSecretKey {
		panic(fmt.Sprintf("%s and %s must both be provided or both be empty", envS3AccessKey, envS3SecretKey))
	}

	return cfg
}
