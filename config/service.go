package config

import (
	"fmt"

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
	cfg := ServiceConfig{
		Port:        GetEnvInt("PORT", defaultPort),
		Environment: GetEnv("ENVIRONMENT", "development"),
		// NO DEFAULT - CORS must be explicitly configured
		AllowedOrigins: GetEnvRequiredList("ALLOWED_ORIGINS"),
		SwaggerHost:    GetEnv("SWAGGER_HOST", ""),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid service configuration: %v", err))
	}

	return cfg
}
