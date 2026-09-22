using System.Net;

namespace Rbac;

/// <summary>
/// Handles automatic CA certificate fetching and caching.
/// </summary>
internal sealed class CACache : IDisposable
{
    private readonly string _baseUrl;
    private readonly string _path;
    private readonly HttpClient _client;

    private const int MaxCACertBytes = 64 << 10; // 64KB

    public CACache(string baseUrl, string path)
    {
        _baseUrl = baseUrl.TrimEnd('/');
        _path = path;
        _client = new HttpClient { Timeout = TimeSpan.FromSeconds(10) };
    }

    /// <summary>
    /// Checks if a CA cert exists at the configured path. If present and non-empty,
    /// reads and returns the bytes. Otherwise, fetches from the server's
    /// /v1/ca-cert endpoint, writes to the local path, and returns the bytes.
    /// </summary>
    public byte[] LoadOrFetch()
    {
        // Check if local file exists
        if (File.Exists(_path))
        {
            var pem = File.ReadAllBytes(_path);
            if (pem.Length > 0)
            {
                return pem;
            }
            // Empty file, treat as missing and refetch
        }

        // File doesn't exist or is empty, fetch from server with retry-once
        return FetchFromServer();
    }

    /// <summary>
    /// Downloads the CA cert from /v1/ca-cert with retry-once logic.
    /// </summary>
    private byte[] FetchFromServer()
    {
        Exception? lastException = null;

        for (int attempt = 0; attempt < 2; attempt++)
        {
            try
            {
                var pem = DoFetch();
                // Write to local path
                File.WriteAllBytes(_path, pem);
                return pem;
            }
            catch (Exception ex)
            {
                lastException = ex;
            }
        }

        throw new ClientConfigException(
            "failed to fetch CA cert after 2 attempts",
            lastException ?? new Exception("unknown error"));
    }

    /// <summary>
    /// Performs a single fetch from /v1/ca-cert.
    /// </summary>
    private byte[] DoFetch()
    {
        var url = $"{_baseUrl}/v1/ca-cert";

        using var request = new HttpRequestMessage(HttpMethod.Get, url);
        using var response = _client.SendAsync(request).GetAwaiter().GetResult();

        if (response.StatusCode != HttpStatusCode.OK)
        {
            throw new ClientConfigException(
                $"fetch CA cert: server returned status {(int)response.StatusCode}");
        }

        using var stream = response.Content.ReadAsStreamAsync().GetAwaiter().GetResult();
        using var ms = new MemoryStream();
        var buffer = new byte[8192];
        long total = 0;

        while (true)
        {
            var n = stream.Read(buffer, 0, buffer.Length);
            if (n == 0) break;

            total += n;
            if (total > MaxCACertBytes)
            {
                ms.Write(buffer, 0, (int)(n - (total - MaxCACertBytes)));
                break;
            }

            ms.Write(buffer, 0, n);
        }

        var data = ms.ToArray();

        if (data.Length == 0)
        {
            throw new ClientConfigException("CA cert endpoint returned empty response");
        }

        return data;
    }

    public void Dispose()
    {
        _client.Dispose();
    }
}
