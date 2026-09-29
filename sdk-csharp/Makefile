# Regenerate gRPC stubs from the vendored proto (run from sdk-csharp/).
# Prefer syncing proto from the monorepo first when working inside the full tree:
#   cp ../server/api/proto/rbac/v1/rbac.proto proto/rbac/v1/rbac.proto
.PHONY: proto
proto:
	dotnet build tools/gen-proto/gen-proto.csproj
	cp src/Rbac.Sdk/Generated/rbac/v1/Rbac.cs src/Rbac.Sdk/Generated/rbac/v1/RbacGrpc.cs src/Rbac.Sdk/Generated/
	rm -rf src/Rbac.Sdk/Generated/rbac
