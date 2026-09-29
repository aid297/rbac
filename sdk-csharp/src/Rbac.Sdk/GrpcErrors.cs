using System.Net;
using Grpc.Core;

namespace Rbac;

/// <summary>
/// Maps gRPC <see cref="RpcException"/> to <see cref="ApiException"/> so
/// <see cref="ApiErrors"/> predicates match the HTTP client.
/// Transport codes (Unavailable, Cancelled, DeadlineExceeded) are rethrown as-is —
/// matching HTTP dial/timeout errors that are not wrapped as <see cref="ApiException"/>.
/// Service pause uses FailedPrecondition on the wire → HTTP 503.
/// </summary>
internal static class GrpcErrors
{
    /// <summary>
    /// Maps application-level gRPC failures to <see cref="ApiException"/>.
    /// Returns false for non-<see cref="RpcException"/> and for transport codes so
    /// callers can <c>throw;</c> and preserve the original stack trace.
    /// </summary>
    public static bool TryMap(string method, Exception ex, out Exception mapped)
    {
        mapped = null!;
        if (ex is not RpcException rpc)
            return false;

        switch (rpc.StatusCode)
        {
            case StatusCode.Unavailable:
            case StatusCode.Cancelled:
            case StatusCode.DeadlineExceeded:
                return false;
        }

        mapped = new ApiException(
            "RPC",
            method,
            GrpcCodeToHttp(rpc.StatusCode),
            Array.Empty<byte>(),
            rpc.Status.Detail ?? rpc.Message);
        return true;
    }

    /// <summary>Maps for tests / callers that already own the exception instance.</summary>
    public static Exception Map(string method, Exception ex) =>
        TryMap(method, ex, out var mapped) ? mapped : ex;

    public static int GrpcCodeToHttp(StatusCode code) => code switch
    {
        StatusCode.OK => (int)HttpStatusCode.OK,
        StatusCode.InvalidArgument or StatusCode.OutOfRange => (int)HttpStatusCode.BadRequest,
        StatusCode.FailedPrecondition => (int)HttpStatusCode.ServiceUnavailable,
        StatusCode.NotFound => (int)HttpStatusCode.NotFound,
        StatusCode.AlreadyExists or StatusCode.Aborted => (int)HttpStatusCode.Conflict,
        StatusCode.PermissionDenied => (int)HttpStatusCode.Forbidden,
        StatusCode.Unauthenticated => (int)HttpStatusCode.Unauthorized,
        StatusCode.ResourceExhausted => (int)HttpStatusCode.TooManyRequests,
        StatusCode.Unimplemented => (int)HttpStatusCode.NotImplemented,
        _ => (int)HttpStatusCode.InternalServerError,
    };
}

public static partial class ApiErrors
{
    /// <summary>
    /// True when <paramref name="ex"/> is a gRPC status error — either mapped to
    /// <see cref="ApiException"/> (Method == "RPC") or a raw <see cref="RpcException"/>
    /// (e.g. Unavailable from dial/transport). Prefer <see cref="IsNotFound"/> /
    /// <see cref="IsPaused"/> for application logic.
    /// </summary>
    public static bool IsGrpc(Exception? ex)
    {
        if (ex is null)
            return false;
        if (ex is ApiException ae && ae.Method == "RPC")
            return true;
        return ex is RpcException;
    }
}
