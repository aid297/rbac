//! In-process gRPC client tests (tonic server on localhost).

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use chrono::{TimeZone, Utc};
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::{transport::Server, Request, Response, Status};

use rbac::pb::rbac::v1::rbac_service_server::{RbacService, RbacServiceServer};
use rbac::pb::rbac::v1::{
    Binding as PbBinding, Condition as PbCondition, EnforceRequest, EnforceResponse,
    GetBindingRequest, GetCaCertRequest, GetCaCertResponse, HealthRequest, HealthResponse,
    ListBindingsRequest, ListBindingsResponse, ReachableRequest, ReachableResponse,
    RemoveBindingRequest, RemoveBindingResponse, SetEnabledRequest, SetEnabledResponse,
};
use rbac::{all_condition, time_range, Binding, CallOpts, Client};

#[derive(Clone, Default)]
struct FakeRbac {
    inner: Arc<Mutex<FakeInner>>,
}

#[derive(Default)]
struct FakeInner {
    bindings: HashMap<String, PbBinding>,
    paused: bool,
    last_enforce: Option<EnforceRequest>,
}

fn key(src: &str, dst: &str, scenario: &str) -> String {
    format!("{src}\0{dst}\0{scenario}")
}

#[tonic::async_trait]
impl RbacService for FakeRbac {
    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        if self.inner.lock().unwrap().paused {
            return Err(Status::failed_precondition("service paused: test"));
        }
        Ok(Response::new(HealthResponse {
            status: "ok".into(),
            reason: String::new(),
        }))
    }

    async fn get_ca_cert(
        &self,
        _request: Request<GetCaCertRequest>,
    ) -> Result<Response<GetCaCertResponse>, Status> {
        Err(Status::unimplemented("not used in tests"))
    }

    async fn enforce(
        &self,
        request: Request<EnforceRequest>,
    ) -> Result<Response<EnforceResponse>, Status> {
        let req = request.into_inner();
        let mut g = self.inner.lock().unwrap();
        g.last_enforce = Some(req.clone());
        if g.paused {
            return Err(Status::failed_precondition("service paused: test"));
        }
        let k = key(&req.subject, &req.target, "");
        let allow = g
            .bindings
            .get(&k)
            .map(|b| b.enabled.unwrap_or(true))
            .unwrap_or(false);
        Ok(Response::new(EnforceResponse { allow }))
    }

    async fn reachable(
        &self,
        request: Request<ReachableRequest>,
    ) -> Result<Response<ReachableResponse>, Status> {
        let req = request.into_inner();
        let g = self.inner.lock().unwrap();
        let mut out = vec![req.subject.clone()];
        for b in g.bindings.values() {
            if b.src == req.subject && b.enabled.unwrap_or(true) {
                out.push(b.dst.clone());
            }
        }
        Ok(Response::new(ReachableResponse {
            subject: req.subject,
            reachable: out,
        }))
    }

    async fn list_bindings(
        &self,
        _request: Request<ListBindingsRequest>,
    ) -> Result<Response<ListBindingsResponse>, Status> {
        let g = self.inner.lock().unwrap();
        Ok(Response::new(ListBindingsResponse {
            bindings: g.bindings.values().cloned().collect(),
        }))
    }

    async fn get_binding(
        &self,
        request: Request<GetBindingRequest>,
    ) -> Result<Response<PbBinding>, Status> {
        let req = request.into_inner();
        let g = self.inner.lock().unwrap();
        g.bindings
            .get(&key(&req.src, &req.dst, &req.scenario))
            .cloned()
            .map(Response::new)
            .ok_or_else(|| Status::not_found("binding not found"))
    }

    async fn add_binding(
        &self,
        request: Request<PbBinding>,
    ) -> Result<Response<PbBinding>, Status> {
        let mut b = request.into_inner();
        let k = key(&b.src, &b.dst, &b.scenario);
        let mut g = self.inner.lock().unwrap();
        if g.bindings.contains_key(&k) {
            return Err(Status::already_exists("duplicate"));
        }
        if b.enabled.is_none() {
            b.enabled = Some(true);
        }
        if b.conditions.is_empty() {
            b.conditions.push(PbCondition {
                kind: "ALL".into(),
                start: None,
                end: None,
            });
        }
        g.bindings.insert(k, b.clone());
        Ok(Response::new(b))
    }

    async fn update_binding(
        &self,
        request: Request<PbBinding>,
    ) -> Result<Response<PbBinding>, Status> {
        let b = request.into_inner();
        let k = key(&b.src, &b.dst, &b.scenario);
        let mut g = self.inner.lock().unwrap();
        if !g.bindings.contains_key(&k) {
            return Err(Status::not_found("binding not found"));
        }
        g.bindings.insert(k, b.clone());
        Ok(Response::new(b))
    }

    async fn set_enabled(
        &self,
        request: Request<SetEnabledRequest>,
    ) -> Result<Response<SetEnabledResponse>, Status> {
        let req = request.into_inner();
        let mut g = self.inner.lock().unwrap();
        let b = g
            .bindings
            .get_mut(&key(&req.src, &req.dst, &req.scenario))
            .ok_or_else(|| Status::not_found("binding not found"))?;
        b.enabled = Some(req.enabled);
        Ok(Response::new(SetEnabledResponse { ok: true }))
    }

    async fn remove_binding(
        &self,
        request: Request<RemoveBindingRequest>,
    ) -> Result<Response<RemoveBindingResponse>, Status> {
        let req = request.into_inner();
        let mut g = self.inner.lock().unwrap();
        if g.bindings
            .remove(&key(&req.src, &req.dst, &req.scenario))
            .is_none()
        {
            return Err(Status::not_found("binding not found"));
        }
        Ok(Response::new(RemoveBindingResponse {}))
    }
}

struct TestServer {
    addr: SocketAddr,
    svc: FakeRbac,
    _join: tokio::task::JoinHandle<()>,
}

impl TestServer {
    fn start(paused: bool) -> Self {
        use std::sync::OnceLock;
        static RT: OnceLock<tokio::runtime::Runtime> = OnceLock::new();
        let rt = RT.get_or_init(|| {
            tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .thread_name("rbac-grpc-test")
                .build()
                .unwrap()
        });
        let listener = rt.block_on(TcpListener::bind("127.0.0.1:0")).unwrap();
        let addr = listener.local_addr().unwrap();
        let svc = FakeRbac {
            inner: Arc::new(Mutex::new(FakeInner {
                paused,
                ..Default::default()
            })),
        };
        let svc2 = svc.clone();
        let join = rt.spawn(async move {
            Server::builder()
                .add_service(RbacServiceServer::new(svc2))
                .serve_with_incoming(TcpListenerStream::new(listener))
                .await
                .unwrap();
        });
        std::thread::sleep(Duration::from_millis(50));
        Self {
            addr,
            svc,
            _join: join,
        }
    }

    fn target(&self) -> String {
        format!("127.0.0.1:{}", self.addr.port())
    }
}

#[test]
fn new_grpc_validation() {
    assert!(Client::new_grpc("").is_err());
    assert!(Client::new_grpc("http://localhost:9080").is_err());
    let err = Client::grpc_builder("localhost:9080")
        .unwrap()
        .ca_cert(&[] as &[u8])
        .unwrap_err();
    assert!(err.to_string().contains("non-empty"));
}

#[test]
fn health_enforce_crud() {
    let server = TestServer::start(false);
    let client = Client::new_grpc(&server.target()).unwrap();

    client.health().unwrap();

    let created = client
        .add_binding(
            &Binding::new("alice", "doc:42")
                .with_enabled(true)
                .with_conditions(vec![all_condition()]),
        )
        .unwrap();
    assert_eq!(created.src, "alice");

    assert!(client
        .enforce("alice", "doc:42", CallOpts::default())
        .unwrap());

    let nodes = client
        .reachable("alice", CallOpts::new().with_scenarios(["VIP"]))
        .unwrap();
    assert_eq!(nodes[0], "alice");

    let got = client.get_binding("alice", "doc:42", "").unwrap();
    assert_eq!(got.dst, "doc:42");

    assert_eq!(client.list_bindings().unwrap().len(), 1);

    let start = Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
    let end = Utc.with_ymd_and_hms(2026, 7, 1, 0, 0, 0).unwrap();
    client
        .update_binding(
            &Binding::new("alice", "doc:42")
                .with_enabled(true)
                .with_conditions(vec![time_range(Some(start), Some(end))]),
        )
        .unwrap();

    client.set_enabled("alice", "doc:42", "", false).unwrap();
    assert!(!client
        .enforce("alice", "doc:42", CallOpts::default())
        .unwrap());

    let err = client
        .add_binding(&Binding::new("alice", "doc:42"))
        .unwrap_err();
    assert!(err.is_conflict());

    client.remove_binding("alice", "doc:42", "").unwrap();
    let err = client.get_binding("alice", "doc:42", "").unwrap_err();
    assert!(err.is_not_found());
}

#[test]
fn paused() {
    let server = TestServer::start(true);
    let client = Client::new_grpc(&server.target()).unwrap();
    let err = client.health().unwrap_err();
    assert!(err.is_paused());
    assert!(err.is_grpc());
}

#[test]
fn close_then_call() {
    let server = TestServer::start(false);
    let client = Client::new_grpc(&server.target()).unwrap();
    client.close();
    assert!(matches!(client.health(), Err(rbac::Error::Closed)));
    client.close();
}

#[test]
fn enforce_with_now() {
    let server = TestServer::start(false);
    let client = Client::new_grpc(&server.target()).unwrap();
    let now = Utc.with_ymd_and_hms(2026, 6, 15, 0, 0, 0).unwrap();
    client
        .enforce(
            "a",
            "b",
            CallOpts::new().with_now(now).with_scenarios(["S"]),
        )
        .unwrap();
    let last = server.svc.inner.lock().unwrap().last_enforce.clone().unwrap();
    assert_eq!(last.subject, "a");
    assert_eq!(last.scenarios, vec!["S"]);
    assert_eq!(last.now.unwrap().seconds, now.timestamp());
}

#[test]
fn timeout_zero_ok() {
    let server = TestServer::start(false);
    let client = Client::grpc_builder(&server.target())
        .unwrap()
        .timeout(Duration::ZERO)
        .build()
        .unwrap();
    client.health().unwrap();
}

#[test]
fn not_found_is_grpc() {
    let server = TestServer::start(false);
    let client = Client::new_grpc(&server.target()).unwrap();
    let err = client.get_binding("no", "such", "").unwrap_err();
    assert!(err.is_not_found());
    assert!(err.is_grpc());
}
