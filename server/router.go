package server

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/adeptry-app/go-common/config"
	"github.com/adeptry-app/go-common/logger"
	"github.com/adeptry-app/go-common/metrics"
	"github.com/adeptry-app/go-common/middleware"
)

// corsAllowedHeaders is the same for every service; make it a ServiceConfig
// field the day one of them needs its own.
const corsAllowedHeaders = "Content-Type,Authorization"

// NewRouter builds the engine every service runs. Without SetTrustedProxies gin
// trusts every peer, so c.ClientIP() returns whatever the caller sends.
func NewRouter(cfg config.ServiceConfig, log *slog.Logger, m *metrics.Metrics) (*gin.Engine, error) {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	router.Use(logger.Recovery(log))
	router.Use(logger.RequestLogger(log))
	router.Use(m.Middleware())
	router.Use(middleware.NewSecurityMiddleware(
		cfg.AllowedOrigins, cfg.AllowedMethods, corsAllowedHeaders, true,
	).Apply())
	return router, nil
}
