//! Automatic CA certificate fetching and caching.

use std::fs;
use std::path::Path;

use reqwest::blocking::Client as HttpClient;

use crate::error::Error;

const MAX_CA_CERT_BYTES: u64 = 64 << 10; // 64KB

/// Handles loading or fetching a CA certificate from the server.
pub struct CACache {
    base_url: String,
    path: String,
    client: HttpClient,
}

impl CACache {
    /// Creates a new CACache that will fetch from `{base_url}/v1/ca-cert`
    /// and store at `path`.
    pub fn new(base_url: &str, path: &str) -> Self {
        let client = HttpClient::builder()
            .timeout(std::time::Duration::from_secs(10))
            .build()
            .expect("failed to build HTTP client for CA cache");

        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            path: path.to_string(),
            client,
        }
    }

    /// Checks if a CA cert exists at the configured path. If present and non-empty,
    /// reads and returns the PEM bytes. Otherwise, fetches from the server's
    /// `/v1/ca-cert` endpoint, writes to the local path, and returns the PEM bytes.
    pub fn load_or_fetch(&self) -> Result<Vec<u8>, Error> {
        // Check if local file exists
        let path = Path::new(&self.path);
        if path.exists() {
            let pem = fs::read(path).map_err(|e| {
                Error::Config(format!("read CA cert from {:?}: {}", self.path, e))
            })?;
            if !pem.is_empty() {
                return Ok(pem);
            }
            // Empty file, treat as missing and refetch
        }

        // File doesn't exist or is empty, fetch from server with retry-once
        self.fetch_from_server()
    }

    /// Downloads the CA cert from `/v1/ca-cert` with retry-once logic.
    fn fetch_from_server(&self) -> Result<Vec<u8>, Error> {
        let mut last_err = None;

        for _attempt in 0..2 {
            match self.do_fetch() {
                Ok(pem) => {
                    // Write to local path
                    fs::write(&self.path, &pem).map_err(|e| {
                        Error::Config(format!("write CA cert to {:?}: {}", self.path, e))
                    })?;
                    return Ok(pem);
                }
                Err(e) => {
                    last_err = Some(e);
                }
            }
        }

        Err(last_err.unwrap_or_else(|| {
            Error::Config("failed to fetch CA cert after 2 attempts".into())
        }))
    }

    /// Performs a single fetch from `/v1/ca-cert`.
    fn do_fetch(&self) -> Result<Vec<u8>, Error> {
        let url = format!("{}/v1/ca-cert", self.base_url);

        let resp = self
            .client
            .get(&url)
            .send()
            .map_err(|e| Error::Config(format!("fetch CA cert: {}", e)))?;

        let status = resp.status();
        if !status.is_success() {
            return Err(Error::Config(format!(
                "fetch CA cert: server returned status {}",
                status.as_u16()
            )));
        }

        let bytes = resp
            .bytes()
            .map_err(|e| Error::Config(format!("read CA cert response: {}", e)))?;

        if bytes.len() > MAX_CA_CERT_BYTES as usize {
            return Err(Error::Config(
                "CA cert response exceeds size limit".into(),
            ));
        }

        if bytes.is_empty() {
            return Err(Error::Config(
                "CA cert endpoint returned empty response".into(),
            ));
        }

        Ok(bytes.to_vec())
    }
}
