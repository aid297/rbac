using System.Net;
using System.Text.Json;

namespace Rbac;

/// <summary>Non-2xx HTTP response from the rbac service.</summary>
public sealed class ApiException : Exception
{
    public int StatusCode { get; }
    public string Method { get; }
    public string Path { get; }
    public string ApiMessage { get; }
    public byte[] Body { get; }

    public ApiException(string method, string path, int statusCode, byte[] body, string message)
        : base(FormatMessage(method, path, statusCode, message))
    {
        Method = method;
        Path = path;
        StatusCode = statusCode;
        Body = body;
        ApiMessage = message;
    }

    private static string FormatMessage(string method, string path, int status, string message) =>
        string.IsNullOrEmpty(message)
            ? $"rbac: {method} {path}: {status}"
            : $"rbac: {method} {path}: {status}: {message}";

    internal static ApiException FromResponse(string method, string path, int status, byte[] body)
    {
        var message = "";
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.TryGetProperty("error", out var err) && err.ValueKind == JsonValueKind.String)
                message = err.GetString() ?? "";
            if (string.IsNullOrEmpty(message)
                && doc.RootElement.TryGetProperty("reason", out var reason)
                && reason.ValueKind == JsonValueKind.String)
                message = reason.GetString() ?? "";
        }
        catch (JsonException)
        {
            // keep empty message; Body still available
        }
        return new ApiException(method, path, status, body, message);
    }
}

/// <summary>Invalid client configuration.</summary>
public sealed class ClientConfigException : Exception
{
    public ClientConfigException(string message) : base($"rbac: {message}") { }
    public ClientConfigException(string message, Exception inner) : base($"rbac: {message}", inner) { }
}

/// <summary>Status-code predicates mirroring the Go SDK helpers.</summary>
public static class ApiErrors
{
    public static bool IsNotFound(Exception? ex) => IsStatus(ex, HttpStatusCode.NotFound);
    public static bool IsConflict(Exception? ex) => IsStatus(ex, HttpStatusCode.Conflict);
    public static bool IsPaused(Exception? ex) => IsStatus(ex, HttpStatusCode.ServiceUnavailable);
    public static bool IsBadRequest(Exception? ex) => IsStatus(ex, HttpStatusCode.BadRequest);

    public static bool IsStatus(Exception? ex, HttpStatusCode code) =>
        ex is ApiException ae && ae.StatusCode == (int)code;

    public static bool IsStatus(Exception? ex, int code) =>
        ex is ApiException ae && ae.StatusCode == code;
}
