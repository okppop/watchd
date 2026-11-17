package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	GIN_LOGGER_KEY = "LOGGER"
	AUTH_COOKIE    = "watchd_session"
)

// LoggerMiddleware makes the process logger available to request handlers.
func LoggerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(GIN_LOGGER_KEY, logger)
		c.Next()
	}
}

// AuthMiddleware accepts a shared login cookie, including native media and WebSocket requests.
func AuthMiddleware(password, token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(AUTH_COOKIE)
		if password != "" && (err != nil || cookie != token) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}
