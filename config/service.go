package config

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// defaultRequestTimeout bounds one request when REQUEST_TIMEOUT is unset.
const defaultRequestTimeout = 30 * time.Second

// defaultMaxBodySize caps a request body when MAX_BODY_SIZE is unset.
const defaultMaxBodySize = 64 << 10 // 64 KiB

// ServiceConfig holds service-level configuration (port, environment, CORS).
// Valid environment values: "development", "staging", "production"
type ServiceConfig struct {
	Port           int      `validate:"required,min=1,max=65535"`
	Environment    string   `validate:"oneof=development staging production"`
	AllowedOrigins []string `validate:"required,min=1,dive,required"`
	SwaggerHost    string   // Optional: Swagger UI host (e.g., "api.example.com"). Empty disables swagger.
	// RequestTimeout is shared by middleware.Timeout, WithStatementTimeout and
	// server.Config.RequestTimeout, so the three deadlines cannot disagree.
	RequestTimeout time.Duration `validate:"min=1s"`
	// MaxBodySize caps a request body; applied by middleware.BodyLimit.
	MaxBodySize int64 `validate:"min=1024"`
}

// NewServiceConfig loads service configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewServiceConfig(defaultPort int) ServiceConfig {
	cfg := ServiceConfig{
		Port:        GetEnvInt("PORT", defaultPort),
		Environment: GetEnv("ENVIRONMENT", "development"),
		// NO DEFAULT - CORS must be explicitly configured
		AllowedOrigins: GetEnvRequiredList("ALLOWED_ORIGINS"),
		SwaggerHost:    GetEnv("SWAGGER_HOST", ""),
		RequestTimeout: GetEnvDuration("REQUEST_TIMEOUT", defaultRequestTimeout),
		MaxBodySize:    GetEnvInt64("MAX_BODY_SIZE", defaultMaxBodySize),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid service configuration: %v", err))
	}

	return cfg
}
