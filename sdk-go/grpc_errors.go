package rbac

import (
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mapGRPCError converts a gRPC status into *APIError so existing IsNotFound /
// IsConflict / IsPaused / IsBadRequest helpers keep working across transports.
//
// Transport-level codes (Unavailable, Canceled, DeadlineExceeded) are returned
// as-is — matching HTTP, where dial/timeout errors are not wrapped as *APIError.
// Service pause uses FailedPrecondition on the wire and maps to HTTP 503.
func mapGRPCError(method string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
		return err
	}
	return &APIError{
		StatusCode: grpcCodeToHTTP(st.Code()),
		Method:     "RPC",
		Path:       method,
		Message:    st.Message(),
	}
}

func grpcCodeToHTTP(c codes.Code) int {
	switch c {
	case codes.OK:
		return http.StatusOK
	case codes.InvalidArgument, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.FailedPrecondition:
		// Service pause (and only pause, for this API).
		return http.StatusServiceUnavailable
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unimplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// IsGRPC reports whether err is a gRPC status error — either already mapped to
// *APIError by this SDK, or a raw status (e.g. Unavailable from dial/transport).
// Prefer IsNotFound / IsPaused etc. for application logic.
func IsGRPC(err error) bool {
	if err == nil {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) && ae.Method == "RPC" {
		return true
	}
	_, ok := status.FromError(err)
	return ok
}
