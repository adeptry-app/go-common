package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// defaultRequestTimeout bounds one request when REQUEST_TIMEOUT is unset.
const defaultRequestTimeout = 30 * time.Second

// defaultMaxBodySize caps a request body when MAX_BODY_SIZE is unset.
const defaultMaxBodySize = 64 << 10 // 64 KiB

// defaultTrustedProxies covers loopback and the private ranges.
var defaultTrustedProxies = []string{
	"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7",
}

// defaultAllowedMethods is the superset; a service with fewer verbs narrows it.
var defaultAllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// ServiceConfig holds service-level configuration (port, environment, CORS).
// Valid environment values: "development", "staging", "production"
type ServiceConfig struct {
	Port        int    `validate:"required,min=1,max=65535"`
	Environment string `validate:"oneof=development staging production"`

	// The CORS pair applied by server.NewRouter. Origins have no default.
	AllowedOrigins []string `validate:"required,min=1,dive,required"`
	AllowedMethods string   `validate:"required"`

	// RequestTimeout is the deadline shared by the handler, pool and socket.
	RequestTimeout time.Duration `validate:"min=1s"`
	// MaxBodySize caps a request body; applied by middleware.BodyLimit.
	MaxBodySize int64 `validate:"min=1024"`
	// TrustedProxies are the CIDRs gin may take X-Forwarded-For from.
	TrustedProxies []string `validate:"required,min=1,dive,cidr"`

	// SwaggerHost is optional (e.g. "api.example.com"); empty disables swagger.
	SwaggerHost string
}

// NewServiceConfig loads service configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewServiceConfig(defaultPort int) ServiceConfig {
	cfg := ServiceConfig{
		Port:        GetEnvInt("PORT", defaultPort),
		Environment: GetEnv("ENVIRONMENT", "development"),
		// NO DEFAULT - CORS must be explicitly configured
		AllowedOrigins: GetEnvRequiredList("ALLOWED_ORIGINS"),
		AllowedMethods: strings.Join(GetEnvList("CORS_ALLOWED_METHODS", defaultAllowedMethods), ","),
		RequestTimeout: GetEnvDuration("REQUEST_TIMEOUT", defaultRequestTimeout),
		MaxBodySize:    GetEnvInt64("MAX_BODY_SIZE", defaultMaxBodySize),
		TrustedProxies: GetEnvList("TRUSTED_PROXIES", defaultTrustedProxies),
		SwaggerHost:    GetEnv("SWAGGER_HOST", ""),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid service configuration: %v", err))
	}

	return cfg
}
