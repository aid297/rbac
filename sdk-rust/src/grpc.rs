//! gRPC transport for [`crate::Client`].

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Timelike, Utc};
use prost_types::Timestamp;
use tokio::runtime::Runtime;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Endpoint};
use tonic::{Code, Request, Status};

use crate::error::{ApiError, Error};
use crate::pb::rbac::v1::rbac_service_client::RbacServiceClient;
use crate::pb::rbac::v1::{
    Binding as PbBinding, Condition as PbCondition, EnforceRequest, GetBindingRequest,
    HealthRequest, ListBindingsRequest, ReachableRequest, RemoveBindingRequest, SetEnabledRequest,
};
use crate::types::{Binding, Condition, ConditionKind};
use crate::CallOpts;

const DEFAULT_USER_AGENT: &str = concat!("rbac-sdk-rust/", env!("CARGO_PKG_VERSION"));
pub(crate) const DEFAULT_TIMEOUT: Duration = Duration::from_secs(30);

/// Shared tokio runtime for blocking gRPC calls (mirrors sync HTTP API).
fn runtime() -> &'static Runtime {
    use std::sync::OnceLock;
    static RT: OnceLock<Runtime> = OnceLock::new();
    RT.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .thread_name("rbac-grpc")
            .build()
            .expect("rbac: failed to create tokio runtime")
    })
}

pub(crate) struct GrpcTransport {
    client: RbacServiceClient<Channel>,
    timeout: Duration,
    closed: AtomicBool,
}

impl GrpcTransport {
    pub(crate) fn close(&self) {
        self.closed.store(true, Ordering::Release);
    }

    fn ensure_open(&self) -> Result<(), Error> {
        if self.closed.load(Ordering::Acquire) {
            return Err(Error::Closed);
        }
        Ok(())
    }

    fn call<F, T>(&self, method: &'static str, fut: F) -> Result<T, Error>
    where
        F: std::future::Future<Output = Result<T, Status>>,
    {
        runtime().block_on(async {
            let result = if self.timeout.is_zero() {
                fut.await
            } else {
                match tokio::time::timeout(self.timeout, fut).await {
                    Ok(r) => r,
                    Err(_) => {
                        return Err(Error::Grpc(Box::new(Status::deadline_exceeded(
                            "rbac: request timed out",
                        ))))
                    }
                }
            };
            result.map_err(|e| map_grpc_error(method, e))
        })
    }

    pub(crate) fn health(&self) -> Result<(), Error> {
        self.ensure_open()?;
        let mut c = self.client.clone();
        self.call("Health", async move {
            c.health(Request::new(HealthRequest {})).await.map(|_| ())
        })
    }

    pub(crate) fn enforce(
        &self,
        subject: &str,
        target: &str,
        opts: CallOpts,
    ) -> Result<bool, Error> {
        self.ensure_open()?;
        let req = EnforceRequest {
            subject: subject.to_string(),
            target: target.to_string(),
            scenarios: opts.scenarios,
            now: opts.now.map(to_timestamp),
        };
        let mut c = self.client.clone();
        let out = self.call("Enforce", async move {
            c.enforce(Request::new(req)).await.map(|r| r.into_inner())
        })?;
        Ok(out.allow)
    }

    pub(crate) fn reachable(&self, subject: &str, opts: CallOpts) -> Result<Vec<String>, Error> {
        self.ensure_open()?;
        let req = ReachableRequest {
            subject: subject.to_string(),
            scenarios: opts.scenarios,
        };
        let mut c = self.client.clone();
        let out = self.call("Reachable", async move {
            c.reachable(Request::new(req)).await.map(|r| r.into_inner())
        })?;
        Ok(out.reachable)
    }

    pub(crate) fn list_bindings(&self) -> Result<Vec<Binding>, Error> {
        self.ensure_open()?;
        let mut c = self.client.clone();
        let out = self.call("ListBindings", async move {
            c.list_bindings(Request::new(ListBindingsRequest {}))
                .await
                .map(|r| r.into_inner())
        })?;
        bindings_from_proto(out.bindings)
    }

    pub(crate) fn get_binding(
        &self,
        src: &str,
        dst: &str,
        scenario: &str,
    ) -> Result<Binding, Error> {
        self.ensure_open()?;
        let req = GetBindingRequest {
            src: src.to_string(),
            dst: dst.to_string(),
            scenario: scenario.to_string(),
        };
        let mut c = self.client.clone();
        let out = self.call("GetBinding", async move {
            c.get_binding(Request::new(req)).await.map(|r| r.into_inner())
        })?;
        binding_from_proto(out)
    }

    pub(crate) fn add_binding(&self, b: &Binding) -> Result<Binding, Error> {
        self.ensure_open()?;
        let req = binding_to_proto(b);
        let mut c = self.client.clone();
        let out = self.call("AddBinding", async move {
            c.add_binding(Request::new(req)).await.map(|r| r.into_inner())
        })?;
        binding_from_proto(out)
    }

    pub(crate) fn update_binding(&self, b: &Binding) -> Result<Binding, Error> {
        self.ensure_open()?;
        let req = binding_to_proto(b);
        let mut c = self.client.clone();
        let out = self.call("UpdateBinding", async move {
            c.update_binding(Request::new(req))
                .await
                .map(|r| r.into_inner())
        })?;
        binding_from_proto(out)
    }

    pub(crate) fn set_enabled(
        &self,
        src: &str,
        dst: &str,
        scenario: &str,
        enabled: bool,
    ) -> Result<(), Error> {
        self.ensure_open()?;
        let req = SetEnabledRequest {
            src: src.to_string(),
            dst: dst.to_string(),
            scenario: scenario.to_string(),
            enabled,
        };
        let mut c = self.client.clone();
        self.call("SetEnabled", async move {
            c.set_enabled(Request::new(req)).await.map(|_| ())
        })
    }

    pub(crate) fn remove_binding(
        &self,
        src: &str,
        dst: &str,
        scenario: &str,
    ) -> Result<(), Error> {
        self.ensure_open()?;
        let req = RemoveBindingRequest {
            src: src.to_string(),
            dst: dst.to_string(),
            scenario: scenario.to_string(),
        };
        let mut c = self.client.clone();
        self.call("RemoveBinding", async move {
            c.remove_binding(Request::new(req)).await.map(|_| ())
        })
    }
}

/// Builder for a gRPC [`crate::Client`].
#[derive(Debug)]
pub struct GrpcBuilder {
    target: String,
    ca_pems: Vec<Vec<u8>>,
    insecure: bool,
    user_agent: String,
    timeout: Duration,
    timeout_set: bool,
}

impl GrpcBuilder {
    pub(crate) fn new(target: &str) -> Result<Self, Error> {
        let target = target.trim().to_string();
        if target.is_empty() {
            return Err(Error::Config("gRPC target must be non-empty".into()));
        }
        if target.contains("://") {
            return Err(Error::Config(format!(
                "gRPC target must be host:port (got {target:?}); use Client::new for http(s):// URLs"
            )));
        }
        Ok(Self {
            target,
            ca_pems: Vec::new(),
            insecure: false,
            user_agent: DEFAULT_USER_AGENT.to_string(),
            timeout: DEFAULT_TIMEOUT,
            timeout_set: false,
        })
    }

    /// Trusts a PEM-encoded CA certificate (enables TLS / `grpc_tls`).
    pub fn ca_cert(mut self, pem: impl AsRef<[u8]>) -> Result<Self, Error> {
        let pem = pem.as_ref();
        if pem.is_empty() {
            return Err(Error::Config("ca_cert requires non-empty PEM data".into()));
        }
        self.ca_pems.push(pem.to_vec());
        Ok(self)
    }

    /// Reads a PEM CA certificate from `path`.
    pub fn ca_cert_file(self, path: impl AsRef<std::path::Path>) -> Result<Self, Error> {
        let path = path.as_ref();
        let pem = std::fs::read(path).map_err(|e| {
            Error::Config(format!("read CA file {}: {e}", path.display()))
        })?;
        self.ca_cert(pem)
    }

    /// Disables TLS certificate verification (testing only; enables TLS).
    pub fn insecure_skip_verify(mut self, insecure: bool) -> Self {
        self.insecure = insecure;
        self
    }

    /// Overrides the default User-Agent.
    pub fn user_agent(mut self, ua: impl Into<String>) -> Self {
        self.user_agent = ua.into();
        self
    }

    /// Sets the overall request timeout (default 30s).
    /// [`Duration::ZERO`] means no timeout (matches Go / C#).
    pub fn timeout(mut self, d: Duration) -> Self {
        self.timeout = d;
        self.timeout_set = true;
        self
    }

    /// Finalizes the gRPC client.
    pub fn build(self) -> Result<crate::Client, Error> {
        let use_tls = !self.ca_pems.is_empty() || self.insecure;
        let uri = if use_tls {
            format!("https://{}", self.target)
        } else {
            format!("http://{}", self.target)
        };

        let ua = if self.user_agent.is_empty() {
            DEFAULT_USER_AGENT
        } else {
            self.user_agent.as_str()
        };

        let endpoint = Endpoint::from_shared(uri.clone())
            .map_err(|e| Error::Config(format!("invalid gRPC endpoint: {e}")))?
            .user_agent(ua)
            .map_err(|e| Error::Config(format!("invalid user-agent: {e}")))?;

        let channel = if self.insecure {
            connect_insecure_tls(endpoint, &self.target)?
        } else if use_tls {
            let mut tls = ClientTlsConfig::new();
            for pem in &self.ca_pems {
                if !pem
                    .windows(b"-----BEGIN CERTIFICATE-----".len())
                    .any(|w| w == b"-----BEGIN CERTIFICATE-----")
                {
                    return Err(Error::Config(
                        "failed to parse CA certificate PEM".into(),
                    ));
                }
                tls = tls.ca_certificate(Certificate::from_pem(pem));
            }
            let endpoint = endpoint
                .tls_config(tls)
                .map_err(|e| Error::Config(format!("TLS config: {e}")))?;
            runtime()
                .block_on(endpoint.connect())
                .map_err(|e| Error::Config(format!("gRPC dial {}: {e}", self.target)))?
        } else {
            runtime()
                .block_on(endpoint.connect())
                .map_err(|e| Error::Config(format!("gRPC dial {}: {e}", self.target)))?
        };

        let timeout = if self.timeout_set {
            self.timeout
        } else {
            DEFAULT_TIMEOUT
        };

        Ok(crate::Client {
            inner: Arc::new(crate::client::ClientInner {
                transport: crate::client::Transport::Grpc(GrpcTransport {
                    client: RbacServiceClient::new(channel),
                    timeout,
                    closed: AtomicBool::new(false),
                }),
            }),
        })
    }
}

fn connect_insecure_tls(endpoint: Endpoint, target: &str) -> Result<Channel, Error> {
    use hyper_util::rt::TokioIo;
    use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
    use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
    use rustls::{DigitallySignedStruct, SignatureScheme};
    use std::sync::Arc;
    use tokio::net::TcpStream;
    use tokio_rustls::TlsConnector;

    #[derive(Debug)]
    struct NoVerifier;

    impl ServerCertVerifier for NoVerifier {
        fn verify_server_cert(
            &self,
            _end_entity: &CertificateDer<'_>,
            _intermediates: &[CertificateDer<'_>],
            _server_name: &ServerName<'_>,
            _ocsp_response: &[u8],
            _now: UnixTime,
        ) -> Result<ServerCertVerified, rustls::Error> {
            Ok(ServerCertVerified::assertion())
        }

        fn verify_tls12_signature(
            &self,
            _message: &[u8],
            _cert: &CertificateDer<'_>,
            _dss: &DigitallySignedStruct,
        ) -> Result<HandshakeSignatureValid, rustls::Error> {
            Ok(HandshakeSignatureValid::assertion())
        }

        fn verify_tls13_signature(
            &self,
            _message: &[u8],
            _cert: &CertificateDer<'_>,
            _dss: &DigitallySignedStruct,
        ) -> Result<HandshakeSignatureValid, rustls::Error> {
            Ok(HandshakeSignatureValid::assertion())
        }

        fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
            rustls::crypto::ring::default_provider()
                .signature_verification_algorithms
                .supported_schemes()
        }
    }

    let _ = rustls::crypto::ring::default_provider().install_default();

    let host = target
        .rsplit_once(':')
        .map(|(h, _)| h.to_string())
        .unwrap_or_else(|| target.to_string());
    let port: u16 = target
        .rsplit_once(':')
        .and_then(|(_, p)| p.parse().ok())
        .unwrap_or(443);

    let tls = rustls::ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(NoVerifier))
        .with_no_client_auth();
    let connector = TlsConnector::from(Arc::new(tls));
    let server_name = ServerName::try_from(host.clone())
        .map_err(|e| Error::Config(format!("invalid TLS server name {host:?}: {e}")))?;

    runtime()
        .block_on(async move {
            endpoint
                .connect_with_connector(tower::service_fn(move |_uri: tonic::transport::Uri| {
                    let connector = connector.clone();
                    let server_name = server_name.clone();
                    let host = host.clone();
                    async move {
                        let addr = format!("{host}:{port}");
                        let tcp = TcpStream::connect(&addr).await?;
                        let tls = connector.connect(server_name, tcp).await?;
                        Ok::<_, std::io::Error>(TokioIo::new(tls))
                    }
                }))
                .await
        })
        .map_err(|e| Error::Config(format!("gRPC dial {target}: {e}")))
}

/// Maps a gRPC [`Status`] to [`Error`]. Transport codes stay as [`Error::Grpc`].
pub(crate) fn map_grpc_error(method: &str, status: Status) -> Error {
    match status.code() {
        Code::Unavailable | Code::Cancelled | Code::DeadlineExceeded => {
            Error::Grpc(Box::new(status))
        }
        _ => Error::Api(ApiError {
            status_code: grpc_code_to_http(status.code()),
            method: "RPC".into(),
            path: method.to_string(),
            message: status.message().to_string(),
            body: Vec::new(),
        }),
    }
}

fn grpc_code_to_http(code: Code) -> u16 {
    match code {
        Code::Ok => 200,
        Code::InvalidArgument | Code::OutOfRange => 400,
        Code::FailedPrecondition => 503,
        Code::NotFound => 404,
        Code::AlreadyExists | Code::Aborted => 409,
        Code::PermissionDenied => 403,
        Code::Unauthenticated => 401,
        Code::ResourceExhausted => 429,
        Code::Unimplemented => 501,
        _ => 500,
    }
}

fn to_timestamp(t: DateTime<Utc>) -> Timestamp {
    let t = t.with_nanosecond(0).expect("truncate to seconds");
    Timestamp {
        seconds: t.timestamp(),
        nanos: 0,
    }
}

fn from_timestamp(t: &Timestamp) -> DateTime<Utc> {
    DateTime::from_timestamp(t.seconds, 0).unwrap_or(DateTime::<Utc>::UNIX_EPOCH)
}

fn binding_to_proto(b: &Binding) -> PbBinding {
    let mut out = PbBinding {
        src: b.src.clone(),
        dst: b.dst.clone(),
        scenario: b.scenario.clone(),
        enabled: b.enabled,
        conditions: Vec::new(),
    };
    for c in &b.conditions {
        let mut pc = PbCondition {
            kind: match c.kind {
                ConditionKind::All => "ALL".into(),
                ConditionKind::Time => "TIME".into(),
            },
            start: None,
            end: None,
        };
        if c.kind == ConditionKind::Time {
            if let Some(s) = c.start {
                pc.start = Some(to_timestamp(s));
            }
            if let Some(e) = c.end {
                pc.end = Some(to_timestamp(e));
            }
        }
        out.conditions.push(pc);
    }
    out
}

fn binding_from_proto(b: PbBinding) -> Result<Binding, Error> {
    let mut conditions = Vec::with_capacity(b.conditions.len());
    for c in b.conditions {
        let kind = ConditionKind::parse(&c.kind).map_err(|msg| {
            Error::Api(ApiError {
                status_code: 400,
                method: "RPC".into(),
                path: "Binding".into(),
                message: msg,
                body: Vec::new(),
            })
        })?;
        conditions.push(Condition {
            kind,
            start: c.start.as_ref().map(from_timestamp),
            end: c.end.as_ref().map(from_timestamp),
        });
    }
    Ok(Binding {
        src: b.src,
        dst: b.dst,
        scenario: b.scenario,
        enabled: b.enabled,
        conditions,
    })
}

fn bindings_from_proto(list: Vec<PbBinding>) -> Result<Vec<Binding>, Error> {
    list.into_iter().map(binding_from_proto).collect()
}

#[cfg(test)]
mod convert_tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn round_trip_truncates_nanos() {
        let start = Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap()
            + chrono::Duration::milliseconds(123);
        let b = Binding::new("a", "b").with_conditions(vec![crate::time_range(Some(start), None)]);
        let pb = binding_to_proto(&b);
        assert_eq!(pb.conditions[0].start.as_ref().unwrap().nanos, 0);
        let back = binding_from_proto(pb).unwrap();
        assert_eq!(
            back.conditions[0].start.unwrap(),
            Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap()
        );
    }

    #[test]
    fn map_unavailable_not_paused() {
        let err = map_grpc_error("Health", Status::unavailable("connection refused"));
        assert!(!err.is_paused());
        assert!(err.is_grpc());
        assert!(matches!(err, Error::Grpc(_)));
    }

    #[test]
    fn map_failed_precondition_paused() {
        let err = map_grpc_error("Health", Status::failed_precondition("service paused: x"));
        assert!(err.is_paused());
        assert!(err.is_grpc());
    }
}
