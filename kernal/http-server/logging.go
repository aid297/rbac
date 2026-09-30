package httpserver

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/aid297/rbac/kernal/logging"
	"github.com/aid297/rbac/kernal/rbac/persist"
)

var apiLog logging.Logger

// SetLogger enables HTTP access/error logging (5xx and 503 pause responses).
func SetLogger(l logging.Logger) {
	apiLog = l
}

func requestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if apiLog == nil {
			return
		}
		status := c.Writer.Status()
		if status < http.StatusInternalServerError && status != http.StatusServiceUnavailable {
			return
		}
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		fields := []any{
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"client", c.ClientIP(),
		}
		if status == http.StatusServiceUnavailable {
			if paused, why := persist.Paused(); paused {
				fields = append(fields, "paused", true, "reason", why)
			}
			apiLog.Warn("http request unavailable", fields...)
			return
		}
		apiLog.Error("http request failed", fields...)
	}
}
