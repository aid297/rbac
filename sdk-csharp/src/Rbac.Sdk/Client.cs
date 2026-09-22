using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Rbac;

/// <summary>
/// Concurrency-safe client for the rbac microservice /v1 API.
/// Construct with <see cref="Create"/> or the constructor overload that accepts <see cref="ClientOptions"/>.
/// </summary>
public sealed class Client : IDisposable
{
    /// <summary>SDK version reported in the default User-Agent.</summary>
    public const string Version = "0.1.0";

    internal const string DefaultUserAgent = "rbac-sdk-csharp/" + Version;
    internal static readonly TimeSpan DefaultTimeout = TimeSpan.FromSeconds(30);
    private const int MaxResponseBytes = 4 << 20;

    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
    };

    private readonly Uri _baseUrl;
    private readonly HttpClient _http;
    private readonly bool _ownsHttpClient;
    private readonly string _userAgent;

    /// <summary>Builds a client for <paramref name="baseUrl"/> with default options (30s timeout).</summary>
    public static Client Create(string baseUrl) => new(baseUrl, new ClientOptions());

    /// <summary>Builds a client for <paramref name="baseUrl"/> with the given options.</summary>
    public Client(string baseUrl, ClientOptions? options = null)
    {
        options ??= new ClientOptions();
        var trimmed = (baseUrl ?? "").Trim();
        if (!Uri.TryCreate(trimmed, UriKind.Absolute, out var uri))
            throw new ClientConfigException($"invalid base URL: {baseUrl}");
        if (uri.Scheme is not ("http" or "https"))
            throw new ClientConfigException($"base URL scheme must be http or https, got \"{uri.Scheme}\"");
        if (string.IsNullOrEmpty(uri.Host))
            throw new ClientConfigException("base URL must include a host");

        if (uri.Scheme == "http" && !options.HttpClientSet && (options.CaCerts.Count > 0 || options.Insecure || options.CaCertPath != null))
            throw new ClientConfigException(
                "TLS options (WithCACert, WithCACertPath, WithInsecureSkipVerify) have no effect with http:// base URL");

        _baseUrl = uri;
        _ownsHttpClient = !options.HttpClientSet;
        _http = options.BuildHttpClient(uri.Scheme, trimmed);
        _userAgent = string.IsNullOrEmpty(options.UserAgent) ? DefaultUserAgent : options.UserAgent;
    }

    /// <summary>Checks service liveness. Throws <see cref="ApiException"/> (paused) on 503.</summary>
    public Task HealthAsync(CancellationToken cancellationToken = default) =>
        DoAsync(HttpMethod.Get, "/healthz", null, null, null, cancellationToken);

    /// <summary>Reports whether subject can reach target under the given options.</summary>
    public async Task<bool> EnforceAsync(
        string subject,
        string target,
        CallOptions? options = null,
        CancellationToken cancellationToken = default)
    {
        options ??= new CallOptions();
        var body = new EnforceRequest
        {
            Subject = subject,
            Target = target,
            Scenarios = options.Scenarios.Count > 0 ? options.Scenarios : null,
            Now = options.Now is { } n ? FormatTime(n) : null,
        };
        var result = await DoJsonAsync<EnforceResponse>(
            HttpMethod.Post, "/v1/enforce", null, body, cancellationToken).ConfigureAwait(false);
        return result?.Allow ?? false;
    }

    /// <summary>Lists every node subject can reach, including subject itself.</summary>
    public async Task<IReadOnlyList<string>> ReachableAsync(
        string subject,
        CallOptions? options = null,
        CancellationToken cancellationToken = default)
    {
        options ??= new CallOptions();
        var query = new QueryBuilder();
        query.Add("subject", subject);
        foreach (var s in options.Scenarios)
            query.Add("scenario", s);
        var result = await DoJsonAsync<ReachableResponse>(
            HttpMethod.Get, "/v1/reachable", query.ToString(), null, cancellationToken).ConfigureAwait(false);
        return result?.Reachable ?? [];
    }

    /// <summary>Returns all bindings.</summary>
    public async Task<IReadOnlyList<Binding>> ListBindingsAsync(CancellationToken cancellationToken = default)
    {
        var result = await DoJsonAsync<BindingsResponse>(
            HttpMethod.Get, "/v1/bindings", null, null, cancellationToken).ConfigureAwait(false);
        return result?.Bindings ?? [];
    }

    /// <summary>Fetches a single binding, or throws not-found <see cref="ApiException"/>.</summary>
    public async Task<Binding> GetBindingAsync(
        string src, string dst, string scenario, CancellationToken cancellationToken = default)
    {
        var result = await DoJsonAsync<Binding>(
            HttpMethod.Get, "/v1/bindings", BindingQuery(src, dst, scenario), null, cancellationToken)
            .ConfigureAwait(false);
        return result ?? new Binding();
    }

    /// <summary>Creates a binding. Duplicate → conflict <see cref="ApiException"/>.</summary>
    public async Task<Binding> AddBindingAsync(Binding binding, CancellationToken cancellationToken = default)
    {
        var result = await DoJsonAsync<Binding>(
            HttpMethod.Post, "/v1/bindings", null, binding, cancellationToken).ConfigureAwait(false);
        return result ?? new Binding();
    }

    /// <summary>Replaces an existing binding. Missing → not-found (not created).</summary>
    public async Task<Binding> UpdateBindingAsync(Binding binding, CancellationToken cancellationToken = default)
    {
        var result = await DoJsonAsync<Binding>(
            HttpMethod.Put, "/v1/bindings", null, binding, cancellationToken).ConfigureAwait(false);
        return result ?? new Binding();
    }

    /// <summary>Toggles a binding's enabled flag.</summary>
    public Task SetEnabledAsync(
        string src, string dst, string scenario, bool enabled,
        CancellationToken cancellationToken = default)
    {
        var body = new SetEnabledRequest
        {
            Src = src,
            Dst = dst,
            Scenario = scenario,
            Enabled = enabled,
        };
        return DoAsync(HttpMethod.Patch, "/v1/bindings/enabled", null, body, null, cancellationToken);
    }

    /// <summary>Deletes a binding, or throws not-found <see cref="ApiException"/>.</summary>
    public Task RemoveBindingAsync(
        string src, string dst, string scenario, CancellationToken cancellationToken = default) =>
        DoAsync(HttpMethod.Delete, "/v1/bindings", BindingQuery(src, dst, scenario), null, null, cancellationToken);

    private async Task<T?> DoJsonAsync<T>(
        HttpMethod method, string path, string? query, object? body, CancellationToken ct)
    {
        T? result = default;
        await DoAsync(method, path, query, body, data =>
        {
            if (data.Length == 0 || IsBlank(data))
                return Task.CompletedTask;
            result = JsonSerializer.Deserialize<T>(data, JsonOptions);
            return Task.CompletedTask;
        }, ct).ConfigureAwait(false);
        return result;
    }

    private static bool IsBlank(byte[] data)
    {
        foreach (var b in data)
        {
            if (b is not ((byte)' ' or (byte)'\t' or (byte)'\n' or (byte)'\r'))
                return false;
        }
        return true;
    }

    private async Task DoAsync(
        HttpMethod method,
        string path,
        string? query,
        object? body,
        Func<byte[], Task>? onOk,
        CancellationToken ct)
    {
        var uri = JoinPath(_baseUrl, path);
        if (!string.IsNullOrEmpty(query))
        {
            var ub = new UriBuilder(uri) { Query = query.TrimStart('?') };
            uri = ub.Uri;
        }

        using var req = new HttpRequestMessage(method, uri);
        req.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));
        if (!string.IsNullOrEmpty(_userAgent))
            req.Headers.TryAddWithoutValidation("User-Agent", _userAgent);

        if (body is not null)
        {
            var bytes = JsonSerializer.SerializeToUtf8Bytes(body, body.GetType(), JsonOptions);
            req.Content = new ByteArrayContent(bytes);
            req.Content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
        }

        using var resp = await _http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct)
            .ConfigureAwait(false);
        {
            var data = await ReadLimitedAsync(resp.Content, ct).ConfigureAwait(false);
            var status = (int)resp.StatusCode;
            if (status is < 200 or > 299)
                throw ApiException.FromResponse(method.Method, path, status, data);
            if (onOk is not null)
                await onOk(data).ConfigureAwait(false);
        }
    }

    private static async Task<byte[]> ReadLimitedAsync(HttpContent content, CancellationToken ct)
    {
        await using var stream = await content.ReadAsStreamAsync(ct).ConfigureAwait(false);
        using var ms = new MemoryStream();
        var buffer = new byte[8192];
        long total = 0;
        while (true)
        {
            var n = await stream.ReadAsync(buffer.AsMemory(0, buffer.Length), ct).ConfigureAwait(false);
            if (n == 0)
                break;
            total += n;
            if (total > MaxResponseBytes)
            {
                ms.Write(buffer, 0, (int)(n - (total - MaxResponseBytes)));
                break;
            }
            ms.Write(buffer, 0, n);
        }
        return ms.ToArray();
    }

    internal static Uri JoinPath(Uri baseUri, string path)
    {
        var baseSegs = baseUri.AbsolutePath.Split('/', StringSplitOptions.RemoveEmptyEntries).ToList();
        foreach (var part in path.TrimStart('/').Split('/', StringSplitOptions.RemoveEmptyEntries))
        {
            if (part == ".")
                continue;
            if (part == "..")
            {
                if (baseSegs.Count > 0)
                    baseSegs.RemoveAt(baseSegs.Count - 1);
                continue;
            }
            baseSegs.Add(part);
        }
        var ub = new UriBuilder(baseUri)
        {
            Path = "/" + string.Join('/', baseSegs),
            Query = "",
            Fragment = "",
        };
        return ub.Uri;
    }

    private static string BindingQuery(string src, string dst, string scenario)
    {
        var q = new QueryBuilder();
        q.Add("src", src);
        q.Add("dst", dst);
        if (!string.IsNullOrEmpty(scenario))
            q.Add("scenario", scenario);
        return q.ToString();
    }

    private static string FormatTime(DateTimeOffset t) =>
        t.UtcDateTime.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'");

    public void Dispose()
    {
        if (_ownsHttpClient)
            _http.Dispose();
    }

    // Expose timeout for tests when we own the client.
    internal TimeSpan HttpTimeout => _http.Timeout;

    private sealed class QueryBuilder
    {
        private readonly List<string> _parts = [];

        public void Add(string key, string value) =>
            _parts.Add($"{Uri.EscapeDataString(key)}={Uri.EscapeDataString(value)}");

        public override string ToString() => string.Join('&', _parts);
    }

    private sealed class EnforceRequest
    {
        [JsonPropertyName("subject")]
        public string Subject { get; set; } = "";

        [JsonPropertyName("target")]
        public string Target { get; set; } = "";

        [JsonPropertyName("scenarios")]
        [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
        public List<string>? Scenarios { get; set; }

        [JsonPropertyName("now")]
        [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
        public string? Now { get; set; }
    }

    private sealed class EnforceResponse
    {
        [JsonPropertyName("allow")]
        public bool Allow { get; set; }
    }

    private sealed class ReachableResponse
    {
        [JsonPropertyName("reachable")]
        public List<string> Reachable { get; set; } = [];
    }

    private sealed class BindingsResponse
    {
        [JsonPropertyName("bindings")]
        public List<Binding> Bindings { get; set; } = [];
    }

    private sealed class SetEnabledRequest
    {
        [JsonPropertyName("src")]
        public string Src { get; set; } = "";

        [JsonPropertyName("dst")]
        public string Dst { get; set; } = "";

        [JsonPropertyName("scenario")]
        public string Scenario { get; set; } = "";

        [JsonPropertyName("enabled")]
        public bool Enabled { get; set; }
    }
}
