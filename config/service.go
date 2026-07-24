package config

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ServiceConfig holds service-level configuration (port, environment, CORS).
// Valid environment values: "development", "staging", "production"
type ServiceConfig struct {
	Port           int      `validate:"required,min=1,max=65535"`
	Environment    string   `validate:"oneof=development staging production"`
	AllowedOrigins []string `validate:"required,min=1,dive,required"`
	SwaggerHost    string   // Optional: Swagger UI host (e.g., "api.example.com"). Empty disables swagger.
}

// NewServiceConfig loads service configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewServiceConfig(defaultPort int) ServiceConfig {
	// Parse allowed origins from comma-separated string
	// NO DEFAULT - CORS must be explicitly configured
	allowedOriginsStr := GetEnvRequired("ALLOWED_ORIGINS")
	rawOrigins := strings.Split(allowedOriginsStr, ",")
	allowedOrigins := make([]string, 0, len(rawOrigins))
	for _, origin := range rawOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowedOrigins = append(allowedOrigins, trimmed)
		}
	}

	cfg := ServiceConfig{
		Port:           GetEnvInt("PORT", defaultPort),
		Environment:    GetEnv("ENVIRONMENT", "development"),
		AllowedOrigins: allowedOrigins,
		SwaggerHost:    GetEnv("SWAGGER_HOST", ""),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid service configuration: %v", err))
	}

	return cfg
}
