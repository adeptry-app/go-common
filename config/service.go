package config

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// defaultRequestTimeout bounds one request when REQUEST_TIMEOUT is unset.
const defaultRequestTimeout = 30 * time.Second

// ServiceConfig holds service-level configuration (port, environment, CORS).
// Valid environment values: "development", "staging", "production"
type ServiceConfig struct {
	Port           int      `validate:"required,min=1,max=65535"`
	Environment    string   `validate:"oneof=development staging production"`
	AllowedOrigins []string `validate:"required,min=1,dive,required"`
	SwaggerHost    string   // Optional: Swagger UI host (e.g., "api.example.com"). Empty disables swagger.
	// RequestTimeout is the one deadline the three appliers share:
	// middleware.Timeout, database.WithStatementTimeout and
	// server.Config.RequestTimeout. Reading it once is what stops them
	// disagreeing - a handler deadline outliving its socket deadline truncates
	// the response mid-body.
	RequestTimeout time.Duration `validate:"min=1s"`
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
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid service configuration: %v", err))
	}

	return cfg
}
