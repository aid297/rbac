# Regenerate gRPC stubs from the vendored proto (run from sdk-rust/).
# Requires protoc + tonic-build (via the gen-proto sub-crate).
# Prefer syncing proto from the monorepo first when working inside the full tree:
#   cp ../server/api/proto/rbac/v1/rbac.proto proto/rbac/v1/rbac.proto
.PHONY: proto
proto:
	cargo run --manifest-path tools/gen-proto/Cargo.toml
	cp tools/gen-proto/target/gen/rbac.v1.rs src/pb/rbac.v1.rs
