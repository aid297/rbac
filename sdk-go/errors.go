package rbac

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError represents a non-2xx response from the rbac service. Message is
// parsed from the service's {"error": "..."} body when present; Body holds the
// raw response for inspection.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
	Body       []byte
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("rbac: %s %s: %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("rbac: %s %s: %d", e.Method, e.Path, e.StatusCode)
}

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool { return isStatus(err, http.StatusNotFound) }

// IsConflict reports whether err is an APIError with status 409 (duplicate binding).
func IsConflict(err error) bool { return isStatus(err, http.StatusConflict) }

// IsPaused reports whether err is an APIError with status 503 (service paused).
func IsPaused(err error) bool { return isStatus(err, http.StatusServiceUnavailable) }

// IsBadRequest reports whether err is an APIError with status 400 (validation).
func IsBadRequest(err error) bool { return isStatus(err, http.StatusBadRequest) }

func isStatus(err error, code int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == code
}
