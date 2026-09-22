using System.Net.Http;
using System.Security.Cryptography.X509Certificates;

namespace Rbac;

/// <summary>Construction-time options for <see cref="Client"/>.</summary>
public sealed class ClientOptions
{
    internal HttpClient? HttpClient { get; private set; }
    internal bool HttpClientSet { get; private set; }
    internal List<byte[]> CaCerts { get; } = [];
    internal bool Insecure { get; private set; }
    internal string UserAgent { get; private set; } = Client.DefaultUserAgent;
    internal TimeSpan Timeout { get; private set; } = Client.DefaultTimeout;
    internal bool TimeoutSet { get; private set; }

    /// <summary>
    /// Supplies your own <see cref="HttpClient"/>. When set, TLS-related options and
    /// <see cref="WithTimeout"/> are ignored; the provided client is used verbatim.
    /// The SDK does not dispose a client supplied this way.
    /// </summary>
    public ClientOptions WithHttpClient(HttpClient httpClient)
    {
        ArgumentNullException.ThrowIfNull(httpClient);
        HttpClient = httpClient;
        HttpClientSet = true;
        return this;
    }

    /// <summary>Trusts a PEM-encoded CA certificate (e.g. the service self-signed CA).</summary>
    public ClientOptions WithCACert(ReadOnlySpan<byte> pem)
    {
        if (pem.IsEmpty)
            throw new ClientConfigException("WithCACert requires non-empty PEM data");
        CaCerts.Add(pem.ToArray());
        return this;
    }

    /// <summary>Trusts a PEM-encoded CA certificate string.</summary>
    public ClientOptions WithCACert(string pem)
    {
        ArgumentException.ThrowIfNullOrEmpty(pem);
        return WithCACert(System.Text.Encoding.UTF8.GetBytes(pem));
    }

    /// <summary>Reads a PEM CA certificate from <paramref name="path"/>.</summary>
    public ClientOptions WithCACertFile(string path)
    {
        try
        {
            return WithCACert(File.ReadAllBytes(path));
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
        {
            throw new ClientConfigException($"read CA file \"{path}\"", ex);
        }
    }

    /// <summary>Disables TLS certificate verification. Intended for testing only.</summary>
    public ClientOptions WithInsecureSkipVerify(bool insecure)
    {
        Insecure = insecure;
        return this;
    }

    /// <summary>Overrides the default User-Agent header.</summary>
    public ClientOptions WithUserAgent(string userAgent)
    {
        UserAgent = userAgent ?? "";
        return this;
    }

    /// <summary>Sets the overall request timeout (default 30s). Ignored when <see cref="WithHttpClient"/> is used.</summary>
    public ClientOptions WithTimeout(TimeSpan timeout)
    {
        Timeout = timeout;
        TimeoutSet = true;
        return this;
    }

    internal HttpClient BuildHttpClient(string scheme)
    {
        if (HttpClientSet)
            return HttpClient!;

        var handler = new HttpClientHandler();
        if (scheme == "https" && (CaCerts.Count > 0 || Insecure))
        {
            if (Insecure)
            {
                handler.ServerCertificateCustomValidationCallback =
                    HttpClientHandler.DangerousAcceptAnyServerCertificateValidator;
            }

            if (CaCerts.Count > 0)
            {
                var roots = new X509Certificate2Collection();
                foreach (var pem in CaCerts)
                {
                    var text = System.Text.Encoding.UTF8.GetString(pem);
                    if (!text.Contains("-----BEGIN CERTIFICATE-----", StringComparison.Ordinal))
                        throw new ClientConfigException("failed to parse CA certificate PEM");
                    try
                    {
                        roots.ImportFromPem(text);
                    }
                    catch (Exception ex)
                    {
                        throw new ClientConfigException("failed to parse CA certificate PEM", ex);
                    }
                }
                if (roots.Count == 0)
                    throw new ClientConfigException("failed to parse CA certificate PEM");

                var trusted = roots;
                handler.ServerCertificateCustomValidationCallback = (message, cert, chain, errors) =>
                {
                    if (cert is null || chain is null)
                        return false;
                    chain.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
                    chain.ChainPolicy.CustomTrustStore.Clear();
                    foreach (var c in trusted)
                        chain.ChainPolicy.CustomTrustStore.Add(c);
                    chain.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
                    return chain.Build(cert);
                };
            }
        }

        var timeout = TimeoutSet ? Timeout : Client.DefaultTimeout;
        return new HttpClient(handler) { Timeout = timeout };
    }
}
