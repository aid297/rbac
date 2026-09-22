//! Client tests mirroring sdk-go coverage (HTTP via mockito).

use std::time::Duration;

use chrono::{TimeZone, Utc};
use serde_json::Value;

use rbac::{
    all_condition, Binding, CallOpts, Client, ConditionKind, ErrorKind,
};

fn client_for(server: &mockito::ServerGuard) -> Client {
    Client::new(&server.url()).expect("Client::new")
}

#[test]
fn new_client_validation() {
    assert!(Client::new("http://localhost:8080").is_ok());
    assert!(Client::new("https://localhost:8443").is_ok());
    assert!(Client::new("").is_err());
    assert!(Client::new("ftp://localhost").is_err());
    assert!(Client::new("http://").is_err());

    let err = Client::builder("https://localhost")
        .unwrap()
        .ca_cert(&[] as &[u8])
        .unwrap_err();
    assert_eq!(err.kind(), ErrorKind::Config);

    let err = Client::builder("https://localhost")
        .unwrap()
        .ca_cert(b"not-pem")
        .unwrap()
        .build()
        .unwrap_err();
    assert_eq!(err.kind(), ErrorKind::Config);

    let err = Client::builder("http://localhost")
        .unwrap()
        .insecure_skip_verify(true)
        .build()
        .unwrap_err();
    assert!(err.to_string().contains("http://"));
}

#[test]
fn health_ok_and_paused() {
    let mut server = mockito::Server::new();
    let _m = server
        .mock("GET", "/healthz")
        .with_status(200)
        .with_body(r#"{"status":"ok"}"#)
        .create();
    client_for(&server).health().unwrap();

    let mut server = mockito::Server::new();
    let _m = server
        .mock("GET", "/healthz")
        .with_status(503)
        .with_body(r#"{"status":"paused","reason":"disk mismatch"}"#)
        .create();
    let err = client_for(&server).health().unwrap_err();
    assert!(err.is_paused());
    match err {
        rbac::Error::Api(ae) => assert_eq!(ae.message, "disk mismatch"),
        other => panic!("want ApiError, got {other}"),
    }
}

#[test]
fn enforce_with_options() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("POST", "/v1/enforce")
        .match_header("content-type", "application/json")
        .with_status(200)
        .with_body(r#"{"allow":true}"#)
        .create();

    let now = Utc.with_ymd_and_hms(2026, 6, 15, 0, 0, 0).unwrap();
    let ok = client_for(&server)
        .enforce(
            "alice",
            "doc:42",
            CallOpts::new()
                .with_scenarios(["VIP", "EU"])
                .with_now(now),
        )
        .unwrap();
    assert!(ok);
    m.assert();
}

#[test]
fn enforce_omits_empty_options() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("POST", "/v1/enforce")
        .with_status(200)
        .with_body(r#"{"allow":false}"#)
        .match_body(mockito::Matcher::Regex(
            r#"\{"subject":"a","target":"b"\}"#.into(),
        ))
        .create();

    let ok = client_for(&server)
        .enforce("a", "b", CallOpts::default())
        .unwrap();
    assert!(!ok);
    m.assert();
}

#[test]
fn reachable_query() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/v1/reachable")
        .match_query(mockito::Matcher::Exact(
            "subject=alice&scenario=VIP&scenario=EU".into(),
        ))
        .with_status(200)
        .with_body(r#"{"subject":"alice","reachable":["alice","doc:42"]}"#)
        .create();

    let got = client_for(&server)
        .reachable("alice", CallOpts::new().with_scenarios(["VIP", "EU"]))
        .unwrap();
    assert_eq!(got, vec!["alice", "doc:42"]);
    m.assert();
}

#[test]
fn binding_crud() {
    let sample = r#"{"src":"alice","dst":"role:editor","scenario":"","enabled":true,"conditions":[{"kind":"ALL"}]}"#;

    let mut server = mockito::Server::new();
    let _m = server
        .mock("GET", "/v1/bindings")
        .match_query(mockito::Matcher::Missing)
        .with_status(200)
        .with_body(format!(r#"{{"bindings":[{sample}]}}"#))
        .create();
    let bs = client_for(&server).list_bindings().unwrap();
    assert_eq!(bs.len(), 1);
    assert_eq!(bs[0].src, "alice");
    assert_eq!(bs[0].conditions[0].kind, ConditionKind::All);

    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/v1/bindings")
        .match_query(mockito::Matcher::AllOf(vec![
            mockito::Matcher::UrlEncoded("src".into(), "alice".into()),
            mockito::Matcher::UrlEncoded("dst".into(), "role:editor".into()),
        ]))
        .with_status(200)
        .with_body(sample)
        .create();
    let b = client_for(&server)
        .get_binding("alice", "role:editor", "")
        .unwrap();
    assert_eq!(b.dst, "role:editor");
    m.assert();

    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/v1/bindings")
        .match_query(mockito::Matcher::Regex(r"src=x&dst=y".into()))
        .with_status(404)
        .with_body(r#"{"error":"binding not found"}"#)
        .create();
    let err = client_for(&server)
        .get_binding("x", "y", "")
        .unwrap_err();
    assert!(err.is_not_found(), "got {err:?}");
    m.assert();

    let mut server = mockito::Server::new();
    let m = server
        .mock("POST", "/v1/bindings")
        .with_status(200)
        .with_body(sample)
        .create();
    let b = Binding::new("alice", "role:editor")
        .with_enabled(true)
        .with_conditions(vec![all_condition()]);
    let out = client_for(&server).add_binding(&b).unwrap();
    assert_eq!(out.src, "alice");
    m.assert();

    let mut server = mockito::Server::new();
    let _m = server
        .mock("POST", "/v1/bindings")
        .with_status(409)
        .with_body(r#"{"error":"duplicate"}"#)
        .create();
    let err = client_for(&server).add_binding(&b).unwrap_err();
    assert!(err.is_conflict());

    let mut server = mockito::Server::new();
    let m = server
        .mock("PUT", "/v1/bindings")
        .with_status(200)
        .with_body(sample)
        .create();
    client_for(&server).update_binding(&b).unwrap();
    m.assert();

    let mut server = mockito::Server::new();
    let m = server
        .mock("PATCH", "/v1/bindings/enabled")
        .match_body(mockito::Matcher::Json(serde_json::json!({
            "src": "alice",
            "dst": "role:editor",
            "scenario": "",
            "enabled": false
        })))
        .with_status(200)
        .with_body("{}")
        .create();
    client_for(&server)
        .set_enabled("alice", "role:editor", "", false)
        .unwrap();
    m.assert();

    let mut server = mockito::Server::new();
    let m = server
        .mock("DELETE", "/v1/bindings")
        .match_query(mockito::Matcher::AllOf(vec![
            mockito::Matcher::UrlEncoded("src".into(), "alice".into()),
            mockito::Matcher::UrlEncoded("dst".into(), "role:editor".into()),
            mockito::Matcher::UrlEncoded("scenario".into(), "VIP".into()),
        ]))
        .with_status(200)
        .with_body("")
        .create();
    client_for(&server)
        .remove_binding("alice", "role:editor", "VIP")
        .unwrap();
    m.assert();
}

#[test]
fn user_agent_and_timeout_options() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/healthz")
        .match_header("user-agent", "my-agent/1")
        .with_status(200)
        .with_body("{}")
        .create();
    let c = Client::builder(&server.url())
        .unwrap()
        .user_agent("my-agent/1")
        .timeout(Duration::from_secs(5))
        .build()
        .unwrap();
    c.health().unwrap();
    m.assert();
}

#[test]
fn base_path_is_preserved() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/prefix/healthz")
        .with_status(200)
        .with_body("{}")
        .create();
    let base = format!("{}/prefix", server.url());
    Client::new(&base).unwrap().health().unwrap();
    m.assert();
}

#[test]
fn bad_request_kind() {
    let mut server = mockito::Server::new();
    let _m = server
        .mock("POST", "/v1/enforce")
        .with_status(400)
        .with_body(r#"{"error":"missing subject"}"#)
        .create();
    let err = client_for(&server)
        .enforce("a", "b", CallOpts::default())
        .unwrap_err();
    assert!(err.is_bad_request());
    assert_eq!(err.kind(), ErrorKind::BadRequest);
}

#[test]
fn ca_cert_file_missing() {
    let err = Client::builder("https://localhost:8443")
        .unwrap()
        .ca_cert_file("/nonexistent/ca.crt")
        .unwrap_err();
    assert_eq!(err.kind(), ErrorKind::Config);
}

#[test]
fn enforce_sends_correct_json_shape() {
    let now = Utc.with_ymd_and_hms(2026, 6, 15, 0, 0, 0).unwrap();
    let mut server = mockito::Server::new();
    let expected = serde_json::json!({
        "subject": "alice",
        "target": "doc:42",
        "scenarios": ["VIP"],
        "now": "2026-06-15T00:00:00Z"
    });
    let m = server
        .mock("POST", "/v1/enforce")
        .match_body(mockito::Matcher::Json(expected))
        .with_status(200)
        .with_body(r#"{"allow":true}"#)
        .create();
    client_for(&server)
        .enforce(
            "alice",
            "doc:42",
            CallOpts::new().with_scenarios(["VIP"]).with_now(now),
        )
        .unwrap();
    m.assert();
}

#[test]
fn list_bindings_request_has_no_query() {
    let mut server = mockito::Server::new();
    let m = server
        .mock("GET", "/v1/bindings")
        .match_query(mockito::Matcher::Missing)
        .with_status(200)
        .with_body(r#"{"bindings":[]}"#)
        .create();
    let v: Value = serde_json::from_str(r#"{"bindings":[]}"#).unwrap();
    assert!(v["bindings"].as_array().unwrap().is_empty());
    client_for(&server).list_bindings().unwrap();
    m.assert();
}
