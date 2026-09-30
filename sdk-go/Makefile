# Regenerate gRPC stubs from the kernal proto (run from sdk-go/).
.PHONY: proto
proto:
	@rm -rf internal/rbacv1/tmp && mkdir -p internal/rbacv1/tmp
	protoc -I ../kernal/api/proto \
		--go_out=internal/rbacv1/tmp --go_opt=paths=source_relative \
		--go_opt=Mrbac/v1/rbac.proto=github.com/aid297/rbac/sdk-go/internal/rbacv1 \
		--go-grpc_out=internal/rbacv1/tmp --go-grpc_opt=paths=source_relative \
		--go-grpc_opt=Mrbac/v1/rbac.proto=github.com/aid297/rbac/sdk-go/internal/rbacv1 \
		rbac/v1/rbac.proto
	mv internal/rbacv1/tmp/rbac/v1/*.go internal/rbacv1/
	rm -rf internal/rbacv1/tmp
