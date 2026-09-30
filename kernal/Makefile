# Build React admin UI into adminui/static/ for //go:embed.
.PHONY: admin-ui
admin-ui:
	cd adminui/web && npm ci && npm run build

# Regenerate protobuf stubs (requires protoc, protoc-gen-go, protoc-gen-go-grpc).
.PHONY: proto
proto:
	protoc \
		--proto_path=api/proto \
		--go_out=api/gen --go_opt=paths=source_relative \
		--go-grpc_out=api/gen --go-grpc_opt=paths=source_relative \
		api/proto/rbac/v1/rbac.proto
