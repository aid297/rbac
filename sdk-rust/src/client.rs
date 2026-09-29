//! HTTP / gRPC client for the rbac `/v1` API.

use std::fs;
use std::path::Path;
use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, SecondsFormat, Utc};
use reqwest::blocking::{Client as HttpClient, ClientBuilder as HttpClientBuilder};
use reqwest::Certificate;
use serde::{Deserialize, Serialize};
use url::Url;

use crate::error::{api_error, Error};
use crate::grpc::GrpcTransport;
use crate::types::Binding;

const DEFAULT_USER_AGENT: &str = concat!("rbac-sdk-rust/", env!("CARGO_PKG_VERSION"));
const DEFAULT_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_RESPONSE_BYTES: u64 = 4 << 20;

/// Per-call options for [`Client::enforce`] and [`Client::reachable`].
#[derive(Debug, Clone, Default)]
pub struct CallOpts {
    pub(crate) scenarios: Vec<String>,
    pub(crate) now: Option<DateTime<Utc>>,
}

impl CallOpts {
    /// Empty options (server uses its own clock; no scenarios).
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets the scenario list.
    pub fn with_scenarios<I, S>(mut self, scenarios: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        self.scenarios = scenarios.into_iter().map(Into::into).collect();
        self
    }

    /// Sets the evaluation timestamp (Enforce only). When omitted, the service
    /// uses its own current time.
    pub fn with_now(mut self, now: DateTime<Utc>) -> Self {
        self.now = Some(now);
        self
    }
}

/// Concurrency-safe client for the rbac microservice `/v1` API.
///
/// Construct with [`Client::new`] / [`Client::builder`] (HTTP) or
/// [`Client::new_grpc`] / [`Client::grpc_builder`] (gRPC).
#[derive(Clone)]
pub struct Client {
    pub(crate) inner: Arc<ClientInner>,
}

impl std::fmt::Debug for Client {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match &self.inner.transport {
            Transport::Http { base_url, .. } => f
                .debug_struct("Client")
                .field("transport", &"http")
                .field("base_url", base_url)
                .finish_non_exhaustive(),
            Transport::Grpc(_) => f
                .debug_struct("Client")
                .field("transport", &"grpc")
                .finish_non_exhaustive(),
        }
    }
}

pub(crate) struct ClientInner {
    pub(crate) transport: Transport,
}

pub(crate) enum Transport {
    Http {
        base_url: Url,
        http: HttpClient,
        user_agent: String,
    },
    Grpc(GrpcTransport),
}

impl Client {
    /// Builds an HTTP client for `base_url` with default options (30s timeout).
    pub fn new(base_url: &str) -> Result<Self, Error> {
        ClientBuilder::new(base_url)?.build()
    }

    /// Starts a builder for custom TLS / timeout / user-agent settings (HTTP).
    pub fn builder(base_url: &str) -> Result<ClientBuilder, Error> {
        ClientBuilder::new(base_url)
    }

    /// Builds a gRPC client for `target` (`host:port`). Plaintext by default.
    pub fn new_grpc(target: &str) -> Result<Self, Error> {
        crate::grpc::GrpcBuilder::new(target)?.build()
    }

    /// Starts a gRPC builder (`host:port`; TLS via CA / insecure options).
    pub fn grpc_builder(target: &str) -> Result<crate::grpc::GrpcBuilder, Error> {
        crate::grpc::GrpcBuilder::new(target)
    }

    /// Releases gRPC resources. No-op for HTTP. Safe to call more than once and
    /// safe to race with in-flight RPCs; subsequent calls return [`Error::Closed`].
    pub fn close(&self) {
        if let Transport::Grpc(g) = &self.inner.transport {
            g.close();
        }
    }

    /// Checks service liveness. Returns `Ok(())` on success, or an API error
    /// ([`Error::is_paused`]) when the service is paused.
    pub fn health(&self) -> Result<(), Error> {
        match &self.inner.transport {
            Transport::Http { .. } => self.do_request("GET", "/healthz", None, None::<()>, false),
            Transport::Grpc(g) => g.health(),
        }
    }

    /// Reports whether `subject` can reach `target` under the given options.
    pub fn enforce(
        &self,
        subject: &str,
        target: &str,
        opts: CallOpts,
    ) -> Result<bool, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.enforce(subject, target, opts),
            Transport::Http { .. } => self.enforce_http(subject, target, opts),
        }
    }

    fn enforce_http(&self, subject: &str, target: &str, opts: CallOpts) -> Result<bool, Error> {
        #[derive(Serialize)]
        struct Body<'a> {
            subject: &'a str,
            target: &'a str,
            #[serde(skip_serializing_if = "Vec::is_empty")]
            scenarios: Vec<String>,
            #[serde(skip_serializing_if = "Option::is_none")]
            now: Option<String>,
        }
        #[derive(Deserialize)]
        struct Out {
            allow: bool,
        }
        let body = Body {
            subject,
            target,
            scenarios: opts.scenarios,
            now: opts
                .now
                .map(|t| t.to_rfc3339_opts(SecondsFormat::Secs, true)),
        };
        let out: Out = self.do_json("POST", "/v1/enforce", None, Some(body))?;
        Ok(out.allow)
    }

    /// Lists every node `subject` can reach, including `subject` itself.
    pub fn reachable(&self, subject: &str, opts: CallOpts) -> Result<Vec<String>, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.reachable(subject, opts),
            Transport::Http { .. } => {
                let mut pairs: Vec<(String, String)> =
                    vec![("subject".into(), subject.into())];
                for s in &opts.scenarios {
                    pairs.push(("scenario".into(), s.clone()));
                }
                #[derive(Deserialize)]
                struct Out {
                    reachable: Vec<String>,
                }
                let out: Out =
                    self.do_json("GET", "/v1/reachable", Some(&pairs), None::<()>)?;
                Ok(out.reachable)
            }
        }
    }

    /// Returns all bindings.
    pub fn list_bindings(&self) -> Result<Vec<Binding>, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.list_bindings(),
            Transport::Http { .. } => {
                #[derive(Deserialize)]
                struct Out {
                    bindings: Vec<Binding>,
                }
                let out: Out = self.do_json("GET", "/v1/bindings", None, None::<()>)?;
                Ok(out.bindings)
            }
        }
    }

    /// Fetches a single binding, or [`Error::is_not_found`] if absent.
    pub fn get_binding(&self, src: &str, dst: &str, scenario: &str) -> Result<Binding, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.get_binding(src, dst, scenario),
            Transport::Http { .. } => {
                let q = binding_query(src, dst, scenario);
                self.do_json("GET", "/v1/bindings", Some(&q), None::<()>)
            }
        }
    }

    /// Creates a binding. A duplicate returns [`Error::is_conflict`].
    pub fn add_binding(&self, b: &Binding) -> Result<Binding, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.add_binding(b),
            Transport::Http { .. } => self.do_json("POST", "/v1/bindings", None, Some(b)),
        }
    }

    /// Replaces an existing binding. Missing → [`Error::is_not_found`] (not created).
    pub fn update_binding(&self, b: &Binding) -> Result<Binding, Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.update_binding(b),
            Transport::Http { .. } => self.do_json("PUT", "/v1/bindings", None, Some(b)),
        }
    }

    /// Toggles a binding's enabled flag.
    pub fn set_enabled(
        &self,
        src: &str,
        dst: &str,
        scenario: &str,
        enabled: bool,
    ) -> Result<(), Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.set_enabled(src, dst, scenario, enabled),
            Transport::Http { .. } => {
                #[derive(Serialize)]
                struct Body<'a> {
                    src: &'a str,
                    dst: &'a str,
                    scenario: &'a str,
                    enabled: bool,
                }
                self.do_request(
                    "PATCH",
                    "/v1/bindings/enabled",
                    None,
                    Some(Body {
                        src,
                        dst,
                        scenario,
                        enabled,
                    }),
                    false,
                )
            }
        }
    }

    /// Deletes a binding, or [`Error::is_not_found`] if absent.
    pub fn remove_binding(&self, src: &str, dst: &str, scenario: &str) -> Result<(), Error> {
        match &self.inner.transport {
            Transport::Grpc(g) => g.remove_binding(src, dst, scenario),
            Transport::Http { .. } => {
                let q = binding_query(src, dst, scenario);
                self.do_request("DELETE", "/v1/bindings", Some(&q), None::<()>, false)
            }
        }
    }

    fn http_parts(&self) -> Result<(&Url, &HttpClient, &str), Error> {
        match &self.inner.transport {
            Transport::Http {
                base_url,
                http,
                user_agent,
            } => Ok((base_url, http, user_agent)),
            Transport::Grpc(_) => Err(Error::Config(
                "internal: HTTP method called on gRPC client".into(),
            )),
        }
    }

    fn do_json<B, T>(
        &self,
        method: &str,
        path: &str,
        query: Option<&[(String, String)]>,
        body: Option<B>,
    ) -> Result<T, Error>
    where
        B: Serialize,
        T: for<'de> Deserialize<'de>,
    {
        let data = self.do_bytes(method, path, query, body)?;
        if data.iter().all(|b| b.is_ascii_whitespace()) {
            return Ok(serde_json::from_value(serde_json::json!({}))?);
        }
        Ok(serde_json::from_slice(&data)?)
    }

    fn do_request<B: Serialize>(
        &self,
        method: &str,
        path: &str,
        query: Option<&[(String, String)]>,
        body: Option<B>,
        _expect_body: bool,
    ) -> Result<(), Error> {
        let _ = self.do_bytes(method, path, query, body)?;
        Ok(())
    }

    fn do_bytes<B: Serialize>(
        &self,
        method: &str,
        path: &str,
        query: Option<&[(String, String)]>,
        body: Option<B>,
    ) -> Result<Vec<u8>, Error> {
        let (base_url, http, user_agent) = self.http_parts()?;
        let mut url = join_path(base_url, path)?;
        if let Some(pairs) = query {
            let mut ser = url.query_pairs_mut();
            for (k, v) in pairs {
                ser.append_pair(k, v);
            }
        }

        let mut builder = http
            .request(method_from_str(method)?, url)
            .header("Accept", "application/json");
        if !user_agent.is_empty() {
            builder = builder.header("User-Agent", user_agent);
        }
        if let Some(b) = body {
            let bytes = serde_json::to_vec(&b)?;
            builder = builder
                .header("Content-Type", "application/json")
                .body(bytes);
        }

        let resp = builder.send()?;
        let status = resp.status().as_u16();
        let bytes = resp.bytes()?;
        let data = if bytes.len() > MAX_RESPONSE_BYTES as usize {
            bytes[..MAX_RESPONSE_BYTES as usize].to_vec()
        } else {
            bytes.to_vec()
        };
        if !(200..300).contains(&status) {
            return Err(Error::Api(api_error(method, path, status, data)));
        }
        Ok(data)
    }
}

fn method_from_str(m: &str) -> Result<reqwest::Method, Error> {
    m.parse()
        .map_err(|_| Error::Config(format!("invalid HTTP method: {m}")))
}

fn join_path(base: &Url, path: &str) -> Result<Url, Error> {
    let mut u = base.clone();
    let mut segs: Vec<String> = u
        .path_segments()
        .map(|s| s.filter(|p| !p.is_empty()).map(str::to_owned).collect())
        .unwrap_or_default();
    for part in path.trim_start_matches('/').split('/') {
        if part.is_empty() || part == "." {
            continue;
        }
        if part == ".." {
            segs.pop();
            continue;
        }
        segs.push(part.to_owned());
    }
    {
        let mut segs_mut = u
            .path_segments_mut()
            .map_err(|_| Error::Config("base URL cannot-be-a-base".into()))?;
        segs_mut.clear();
        for s in &segs {
            segs_mut.push(s);
        }
    }
    Ok(u)
}

fn binding_query(src: &str, dst: &str, scenario: &str) -> Vec<(String, String)> {
    let mut q = vec![
        ("src".into(), src.into()),
        ("dst".into(), dst.into()),
    ];
    if !scenario.is_empty() {
        q.push(("scenario".into(), scenario.into()));
    }
    q
}

/// Builder for an HTTP [`Client`].
#[derive(Debug)]
pub struct ClientBuilder {
    base_url: Url,
    custom_http: Option<HttpClient>,
    ca_pems: Vec<Vec<u8>>,
    ca_cert_path: Option<String>,
    insecure: bool,
    user_agent: String,
    timeout: Duration,
    timeout_set: bool,
}

impl ClientBuilder {
    fn new(base_url: &str) -> Result<Self, Error> {
        let trimmed = base_url.trim();
        let u = Url::parse(trimmed).map_err(|e| Error::Config(format!("invalid base URL: {e}")))?;
        if u.scheme() != "http" && u.scheme() != "https" {
            return Err(Error::Config(format!(
                "base URL scheme must be http or https, got {:?}",
                u.scheme()
            )));
        }
        if u.host_str().is_none() {
            return Err(Error::Config("base URL must include a host".into()));
        }
        Ok(Self {
            base_url: u,
            custom_http: None,
            ca_pems: Vec::new(),
            ca_cert_path: None,
            insecure: false,
            user_agent: DEFAULT_USER_AGENT.to_string(),
            timeout: DEFAULT_TIMEOUT,
            timeout_set: false,
        })
    }

    /// Supplies your own blocking HTTP client. When set, TLS-related options
    /// and [`Self::timeout`] are ignored.
    pub fn http_client(mut self, client: HttpClient) -> Self {
        self.custom_http = Some(client);
        self
    }

    /// Trusts a PEM-encoded CA certificate (e.g. the service self-signed CA).
    /// Only valid with an `https://` base URL.
    pub fn ca_cert(mut self, pem: impl AsRef<[u8]>) -> Result<Self, Error> {
        let pem = pem.as_ref();
        if pem.is_empty() {
            return Err(Error::Config("ca_cert requires non-empty PEM data".into()));
        }
        self.ca_pems.push(pem.to_vec());
        Ok(self)
    }

    /// Reads a PEM CA certificate from `path`.
    pub fn ca_cert_file(self, path: impl AsRef<Path>) -> Result<Self, Error> {
        let path = path.as_ref();
        let pem = fs::read(path).map_err(|e| {
            Error::Config(format!("read CA file {}: {e}", path.display()))
        })?;
        self.ca_cert(pem)
    }

    /// Configures automatic CA certificate management. The SDK will check if a
    /// CA cert exists at the given path; if missing or empty, it fetches from
    /// the server's `/v1/ca-cert` endpoint and caches it locally. On download
    /// failure, it retries once before failing permanently.
    pub fn ca_cert_path(mut self, path: impl Into<String>) -> Self {
        self.ca_cert_path = Some(path.into());
        self
    }

    /// Disables TLS certificate verification (testing only).
    pub fn insecure_skip_verify(mut self, insecure: bool) -> Self {
        self.insecure = insecure;
        self
    }

    /// Overrides the default User-Agent (`rbac-sdk-rust/<version>`).
    pub fn user_agent(mut self, ua: impl Into<String>) -> Self {
        self.user_agent = ua.into();
        self
    }

    /// Sets the overall request timeout (default 30s). Ignored when
    /// [`Self::http_client`] is used. [`Duration::ZERO`] means no timeout.
    pub fn timeout(mut self, d: Duration) -> Self {
        self.timeout = d;
        self.timeout_set = true;
        self
    }

    /// Finalizes the HTTP client.
    pub fn build(self) -> Result<Client, Error> {
        let scheme = self.base_url.scheme();
        if scheme == "http"
            && self.custom_http.is_none()
            && (!self.ca_pems.is_empty() || self.insecure || self.ca_cert_path.is_some())
        {
            return Err(Error::Config(
                "TLS options (ca_cert, ca_cert_path, insecure_skip_verify) have no effect with http:// base URL"
                    .into(),
            ));
        }

        let mut ca_pems = self.ca_pems;
        if let Some(ref path) = self.ca_cert_path {
            if self.custom_http.is_none() && scheme == "https" {
                let cache = crate::cacache::CACache::new(self.base_url.as_ref(), path);
                let pem = cache.load_or_fetch()?;
                ca_pems.push(pem);
            }
        }

        let http = if let Some(hc) = self.custom_http {
            hc
        } else {
            let timeout = if self.timeout_set {
                self.timeout
            } else {
                DEFAULT_TIMEOUT
            };
            let mut b = HttpClientBuilder::new().user_agent("");
            // reqwest: None disables timeout; ZERO is rejected — map to None.
            b = if timeout.is_zero() {
                b.timeout(None)
            } else {
                b.timeout(timeout)
            };
            if scheme == "https" && (!ca_pems.is_empty() || self.insecure) {
                if self.insecure {
                    b = b.danger_accept_invalid_certs(true);
                }
                for pem in &ca_pems {
                    let cert = parse_ca_pem(pem)?;
                    b = b.add_root_certificate(cert);
                }
            }
            b.build()?
        };

        let user_agent = if self.user_agent.is_empty() {
            DEFAULT_USER_AGENT.to_string()
        } else {
            self.user_agent
        };

        Ok(Client {
            inner: Arc::new(ClientInner {
                transport: Transport::Http {
                    base_url: self.base_url,
                    http,
                    user_agent,
                },
            }),
        })
    }
}

fn parse_ca_pem(pem: &[u8]) -> Result<Certificate, Error> {
    const BEGIN: &[u8] = b"-----BEGIN CERTIFICATE-----";
    if !pem.windows(BEGIN.len()).any(|w| w == BEGIN) {
        return Err(Error::Config(
            "failed to parse CA certificate PEM".into(),
        ));
    }
    Certificate::from_pem(pem).map_err(|e| {
        Error::Config(format!("failed to parse CA certificate PEM: {e}"))
    })
}
