use rbac::CACache;
use std::fs;

fn make_temp_path(test_name: &str) -> std::path::PathBuf {
    let id = std::process::id();
    let nanos = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    std::env::temp_dir().join(format!("rbac-test-{}-{}-{}", test_name, id, nanos))
}

#[test]
fn test_cacache_load_or_fetch_local_miss() {
    let mut server = mockito::Server::new();
    
    let _mock = server
        .mock("GET", "/v1/ca-cert")
        .with_status(200)
        .with_header("content-type", "application/x-pem-file")
        .with_body("test-ca-cert")
        .create();

    let temp_dir = make_temp_path("local-miss");
    fs::create_dir_all(&temp_dir).unwrap();
    let ca_path = temp_dir.join("ca.pem");

    let cache = CACache::new(&server.url(), &ca_path.to_string_lossy());
    let pem = cache.load_or_fetch().unwrap();

    assert_eq!(String::from_utf8_lossy(&pem), "test-ca-cert");

    // Verify file was written
    let cached = fs::read(&ca_path).unwrap();
    assert_eq!(String::from_utf8_lossy(&cached), "test-ca-cert");
    
    // Cleanup
    fs::remove_dir_all(&temp_dir).ok();
}

#[test]
fn test_cacache_load_or_fetch_local_hit() {
    let server = mockito::Server::new();

    // Pre-create local CA cert
    let temp_dir = make_temp_path("local-hit");
    fs::create_dir_all(&temp_dir).unwrap();
    let ca_path = temp_dir.join("ca.pem");
    let expected_pem = b"existing-ca-cert";
    fs::write(&ca_path, expected_pem).unwrap();

    let cache = CACache::new(&server.url(), &ca_path.to_string_lossy());
    let pem = cache.load_or_fetch().unwrap();

    assert_eq!(String::from_utf8_lossy(&pem), "existing-ca-cert");
    
    // Cleanup
    fs::remove_dir_all(&temp_dir).ok();
}

#[test]
fn test_cacache_load_or_fetch_empty_local_file() {
    let mut server = mockito::Server::new();
    
    let _mock = server
        .mock("GET", "/v1/ca-cert")
        .with_status(200)
        .with_header("content-type", "application/x-pem-file")
        .with_body("fetched-cert")
        .create();

    let temp_dir = make_temp_path("empty-file");
    fs::create_dir_all(&temp_dir).unwrap();
    let ca_path = temp_dir.join("ca.pem");

    // Create empty file
    fs::write(&ca_path, []).unwrap();

    let cache = CACache::new(&server.url(), &ca_path.to_string_lossy());
    let pem = cache.load_or_fetch().unwrap();

    assert_eq!(String::from_utf8_lossy(&pem), "fetched-cert");
    
    // Cleanup
    fs::remove_dir_all(&temp_dir).ok();
}

#[test]
fn test_cacache_fetch_from_server_non_ok_status() {
    let mut server = mockito::Server::new();
    
    let _mock = server
        .mock("GET", "/v1/ca-cert")
        .with_status(404)
        .create();

    let temp_dir = make_temp_path("non-ok");
    fs::create_dir_all(&temp_dir).unwrap();
    let ca_path = temp_dir.join("ca.pem");

    let cache = CACache::new(&server.url(), &ca_path.to_string_lossy());
    let result = cache.load_or_fetch();

    assert!(result.is_err());
    
    // Cleanup
    fs::remove_dir_all(&temp_dir).ok();
}

#[test]
fn test_cacache_fetch_from_server_empty_response() {
    let mut server = mockito::Server::new();
    
    let _mock = server
        .mock("GET", "/v1/ca-cert")
        .with_status(200)
        .with_header("content-type", "application/x-pem-file")
        .with_body("")
        .create();

    let temp_dir = make_temp_path("empty-response");
    fs::create_dir_all(&temp_dir).unwrap();
    let ca_path = temp_dir.join("ca.pem");

    let cache = CACache::new(&server.url(), &ca_path.to_string_lossy());
    let result = cache.load_or_fetch();

    assert!(result.is_err());
    
    // Cleanup
    fs::remove_dir_all(&temp_dir).ok();
}
