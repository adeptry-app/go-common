package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

// DatabaseConfig holds PostgreSQL database configuration
type DatabaseConfig struct {
	Host     string `validate:"required"`
	Port     int    `validate:"required,min=1,max=65535"`
	User     string `validate:"required"`
	Password string `validate:"required"`
	Name     string `validate:"required"`
	SSLMode  string `validate:"omitempty,oneof=disable allow prefer require verify-ca verify-full"` // optional, defaults to "disable" for local, set to "require" for AWS RDS
}

// NewDatabaseConfig loads database configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewDatabaseConfig() DatabaseConfig {
	cfg := DatabaseConfig{
		Host:     GetEnvRequired("DB_HOST"),
		Port:     GetEnvRequiredInt("DB_PORT"),
		User:     GetEnvRequired("DB_USER"),
		Password: GetEnvRequired("DB_PASSWORD"),
		Name:     GetEnvRequired("DB_NAME"),
		SSLMode:  GetEnv("DB_SSLMODE", "disable"),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid database configuration: %v", err))
	}

	return cfg
}
