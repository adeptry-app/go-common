package config

import (
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
)

// JWTConfig holds the token issuer's configuration. Verifying services need only
// the public keys, via GetEnvRequired("JWT_PUBLIC_KEYS").
type JWTConfig struct {
	PrivateKey     string
	PublicKeys     string
	AccessAudience []string      `validate:"required,min=1,dive,required"`
	AccessExpiry   time.Duration `validate:"gt=0"`
	RefreshExpiry  time.Duration `validate:"gt=0"`
}

// NewJWTConfig loads JWT configuration from environment variables.
// Default values:
//   - JWT_ACCESS_EXPIRY: 15m (15 minutes)
//   - JWT_REFRESH_EXPIRY: 168h (7 days)
func NewJWTConfig() JWTConfig {
	cfg := JWTConfig{
		PrivateKey: GetEnvRequired("JWT_PRIVATE_KEY"),
		PublicKeys: GetEnvRequired("JWT_PUBLIC_KEYS"),
		// NO DEFAULT - every service the access cookie reaches must be listed.
		AccessAudience: GetEnvRequiredList("JWT_ACCESS_AUDIENCE"),
		AccessExpiry:   GetEnvDuration("JWT_ACCESS_EXPIRY", 15*time.Minute),
		RefreshExpiry:  GetEnvDuration("JWT_REFRESH_EXPIRY", 168*time.Hour),
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		panic(fmt.Sprintf("Invalid JWT configuration: %v", err))
	}

	return cfg
}
