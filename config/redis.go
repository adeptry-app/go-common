package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string `validate:"required"`
	Port     int    `validate:"required,min=1,max=65535"`
	Password string // Optional, no validation
	TLS      bool   // REDIS_TLS, default false
}

// NewRedisConfig loads Redis configuration from environment variables.
// It panics if required environment variables are missing or configuration is invalid.
func NewRedisConfig() RedisConfig {
	cfg := RedisConfig{
		Host:     GetEnvRequired("REDIS_HOST"),
		Port:     GetEnvRequiredInt("REDIS_PORT"),
		Password: GetEnv("REDIS_PASSWORD", ""),
		TLS:      GetEnvBool("REDIS_TLS", false),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid Redis configuration: %v", err))
	}

	return cfg
}
