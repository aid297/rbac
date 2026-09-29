//! Error types returned by the rbac client.

use std::fmt;

use tonic::Status;

/// Client and API errors.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// Invalid base URL or client configuration.
    #[error("rbac: {0}")]
    Config(String),

    /// Non-2xx response from the rbac service (HTTP or mapped gRPC).
    #[error("{0}")]
    Api(#[from] ApiError),

    /// Transport / TLS / timeout / I/O failure (HTTP).
    #[error("rbac: transport: {0}")]
    Transport(#[from] reqwest::Error),

    /// Raw gRPC transport status (Unavailable / Cancelled / DeadlineExceeded).
    /// Application statuses are mapped to [`Error::Api`] instead. Boxed to
    /// keep `Error` small (avoids `clippy::result_large_err`).
    #[error("rbac: grpc: {0}")]
    Grpc(#[from] Box<Status>),

    /// gRPC client was closed via [`crate::Client::close`].
    #[error("rbac: client is closed")]
    Closed,

    /// Failed to encode or decode JSON.
    #[error("rbac: json: {0}")]
    Json(#[from] serde_json::Error),

    /// Failed to read a file (e.g. CA certificate).
    #[error("rbac: io: {0}")]
    Io(#[from] std::io::Error),
}

/// Non-2xx HTTP response from the service (or mapped gRPC application error).
#[derive(Debug, Clone)]
#[allow(clippy::module_name_repetitions)]
pub struct ApiError {
    /// HTTP status code (gRPC application codes are mapped to HTTP).
    pub status_code: u16,
    /// Request method (`"RPC"` for gRPC).
    pub method: String,
    /// Request path (gRPC method name when `method == "RPC"`).
    pub path: String,
    /// Message from `{"error":...}` / `{"reason":...}` or gRPC status detail.
    pub message: String,
    /// Raw response body (empty for mapped gRPC errors).
    pub body: Vec<u8>,
}

impl fmt::Display for ApiError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.message.is_empty() {
            write!(
                f,
                "rbac: {} {}: {}",
                self.method, self.path, self.status_code
            )
        } else {
            write!(
                f,
                "rbac: {} {}: {}: {}",
                self.method, self.path, self.status_code, self.message
            )
        }
    }
}

impl std::error::Error for ApiError {}

impl Error {
    /// Returns true if this is an API error with the given status.
    pub fn is_status(&self, code: u16) -> bool {
        matches!(self, Error::Api(e) if e.status_code == code)
    }

    /// 404 — binding not found.
    pub fn is_not_found(&self) -> bool {
        self.is_status(404)
    }

    /// 409 — duplicate binding.
    pub fn is_conflict(&self) -> bool {
        self.is_status(409)
    }

    /// 503 — service paused.
    pub fn is_paused(&self) -> bool {
        self.is_status(503)
    }

    /// 400 — validation / bad request.
    pub fn is_bad_request(&self) -> bool {
        self.is_status(400)
    }

    /// True when the error came from the gRPC path (mapped [`Error::Api`] with
    /// `method == "RPC"`, or a raw transport [`Error::Grpc`]).
    pub fn is_grpc(&self) -> bool {
        match self {
            Error::Api(e) if e.method == "RPC" => true,
            Error::Grpc(_) => true,
            _ => false,
        }
    }
    /// Classifies the error for `match`-style handling.
    pub fn kind(&self) -> ErrorKind {
        match self {
            Error::Api(e) => match e.status_code {
                400 => ErrorKind::BadRequest,
                404 => ErrorKind::NotFound,
                409 => ErrorKind::Conflict,
                503 => ErrorKind::Paused,
                _ => ErrorKind::Api,
            },
            Error::Config(_) => ErrorKind::Config,
            Error::Transport(_) | Error::Grpc(_) => ErrorKind::Transport,
            Error::Closed => ErrorKind::Closed,
            Error::Json(_) => ErrorKind::Json,
            Error::Io(_) => ErrorKind::Io,
        }
    }
}

/// Coarse error category (mirrors Go `Is*` helpers).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ErrorKind {
    /// Invalid client configuration.
    Config,
    /// HTTP 400.
    BadRequest,
    /// HTTP 404.
    NotFound,
    /// HTTP 409.
    Conflict,
    /// HTTP 503.
    Paused,
    /// Other non-2xx API status.
    Api,
    /// Network / TLS / timeout / gRPC transport.
    Transport,
    /// Client closed.
    Closed,
    /// JSON encode/decode.
    Json,
    /// Local I/O.
    Io,
}

pub(crate) fn api_error(method: &str, path: &str, status: u16, body: Vec<u8>) -> ApiError {
    let mut message = String::new();
    if let Ok(v) = serde_json::from_slice::<serde_json::Value>(&body) {
        if let Some(e) = v.get("error").and_then(|x| x.as_str()) {
            if !e.is_empty() {
                message = e.to_string();
            }
        }
        if message.is_empty() {
            if let Some(r) = v.get("reason").and_then(|x| x.as_str()) {
                message = r.to_string();
            }
        }
    }
    ApiError {
        status_code: status,
        method: method.to_string(),
        path: path.to_string(),
        message,
        body,
    }
}
