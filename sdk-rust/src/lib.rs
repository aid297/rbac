//! Official Rust client for the rbac authorization microservice `/v1` API.
//!
//! Mirrors the Go SDK (`sdk-go`): HTTP/HTTPS, self-signed CA trust, binding CRUD,
//! Enforce / Reachable, and typed API errors.
//!
//! ```no_run
//! use rbac::{CallOpts, Client};
//!
//! let client = Client::new("http://localhost:8080")?;
//! let allow = client.enforce("alice", "doc:42", CallOpts::default())?;
//! # Ok::<(), rbac::Error>(())
//! ```

#![deny(missing_docs)]

mod cacache;
mod client;
mod error;
mod types;

pub use cacache::CACache;
pub use client::{CallOpts, Client, ClientBuilder};
pub use error::{ApiError, Error, ErrorKind};
pub use types::{all_condition, time_range, Binding, Condition, ConditionKind};

/// SDK version reported in the default `User-Agent` header.
pub const VERSION: &str = env!("CARGO_PKG_VERSION");
