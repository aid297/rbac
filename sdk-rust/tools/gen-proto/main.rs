//! Regenerates `src/pb/rbac.v1.rs` from `proto/rbac/v1/rbac.proto`.
//!
//! Run via `make proto` from `sdk-rust/`. Requires `protoc` on PATH.
use std::path::PathBuf;

fn main() {
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let proto_root = manifest_dir.join("../../proto");
    let proto = proto_root.join("rbac/v1/rbac.proto");
    let out_dir = manifest_dir.join("target/gen");
    std::fs::create_dir_all(&out_dir).expect("create out dir");

    tonic_build::configure()
        .build_client(true)
        .build_server(true)
        .out_dir(&out_dir)
        .compile_protos(&[proto], &[proto_root])
        .expect("tonic-build compile");

    println!("Generated: {}", out_dir.join("rbac.v1.rs").display());
}
