using System.Net;
using System.Text;
using System.Text.Json;
using Rbac;

namespace Rbac.Sdk.Tests;

public class TypesTests
{
    [Fact]
    public void Condition_Marshal_Shapes()
    {
        Assert.Equal("""{"kind":"ALL"}""", JsonSerializer.Serialize(Condition.All()));

        var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero);
        var end = new DateTimeOffset(2026, 7, 1, 0, 0, 0, TimeSpan.Zero);
        Assert.Equal(
            """{"kind":"TIME","start":"2026-06-01T00:00:00Z","end":"2026-07-01T00:00:00Z"}""",
            JsonSerializer.Serialize(Condition.TimeRange(start, end)));
        Assert.Equal(
            """{"kind":"TIME","start":"2026-06-01T00:00:00Z"}""",
            JsonSerializer.Serialize(Condition.TimeRange(start, null)));
    }

    [Fact]
    public void Condition_Rejects_Unknown_Kind()
    {
        var ex = Assert.ThrowsAny<JsonException>(
            () => JsonSerializer.Deserialize<Condition>("""{"kind":"BOGUS"}"""));
        Assert.Contains("unknown condition kind", ex.Message);
    }

    [Fact]
    public void Binding_Omits_Enabled_When_Null()
    {
        var b = new Binding { Src = "a", Dst = "b" };
        using var doc = JsonDocument.Parse(JsonSerializer.Serialize(b));
        Assert.False(doc.RootElement.TryGetProperty("enabled", out _));

        var b2 = new Binding { Src = "a", Dst = "b", Enabled = false };
        using var doc2 = JsonDocument.Parse(JsonSerializer.Serialize(b2));
        Assert.False(doc2.RootElement.GetProperty("enabled").GetBoolean());
    }

    [Fact]
    public void Binding_RoundTrip()
    {
        var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero);
        var b = new Binding
        {
            Src = "alice",
            Dst = "role:editor",
            Scenario = "VIP",
            Enabled = true,
            Conditions = [Condition.TimeRange(start, null)],
        };
        var json = JsonSerializer.Serialize(b);
        var back = JsonSerializer.Deserialize<Binding>(json)!;
        Assert.Equal("alice", back.Src);
        Assert.True(back.Enabled);
        Assert.Equal(start, back.Conditions[0].Start);
    }
}

public class ClientTests
{
    private static (Client Client, RecordingHandler Handler) TestClient(
        Func<HttpRequestMessage, HttpResponseMessage> respond,
        ClientOptions? opts = null)
    {
        var handler = new RecordingHandler(respond);
        var http = new HttpClient(handler) { BaseAddress = new Uri("http://example.invalid/") };
        opts ??= new ClientOptions();
        opts.WithHttpClient(http);
        // Base URL host is ignored when HttpClient has a custom handler that doesn't dial;
        // we still need a valid absolute URL for request building.
        var client = new Client("http://localhost:8080", opts);
        return (client, handler);
    }

    private static HttpResponseMessage Json(HttpStatusCode status, string body)
    {
        var resp = new HttpResponseMessage(status)
        {
            Content = new StringContent(body, Encoding.UTF8, "application/json"),
        };
        return resp;
    }

    [Fact]
    public void NewClient_Validation()
    {
        Assert.NotNull(Client.Create("http://localhost:8080"));
        Assert.NotNull(Client.Create("https://localhost:8443"));
        Assert.Throws<ClientConfigException>(() => Client.Create(""));
        Assert.Throws<ClientConfigException>(() => Client.Create("ftp://localhost"));
        Assert.Throws<ClientConfigException>(() => Client.Create("http://"));
        Assert.Throws<ClientConfigException>(() =>
            new Client("https://localhost", new ClientOptions().WithCACert(ReadOnlySpan<byte>.Empty)));
        Assert.Throws<ClientConfigException>(() =>
            new Client("https://localhost", new ClientOptions().WithCACert("not-pem")));
        Assert.Throws<ClientConfigException>(() =>
            new Client("http://localhost", new ClientOptions().WithInsecureSkipVerify(true)));
        Assert.Throws<ClientConfigException>(() =>
            new Client("http://localhost", new ClientOptions().WithCACert("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")));
    }

    [Fact]
    public async Task Health_Ok_And_Paused()
    {
        var (c, _) = TestClient(_ => Json(HttpStatusCode.OK, """{"status":"ok"}"""));
        await c.HealthAsync();

        var (c2, _) = TestClient(_ => Json(HttpStatusCode.ServiceUnavailable,
            """{"status":"paused","reason":"disk mismatch"}"""));
        var ex = await Assert.ThrowsAsync<ApiException>(() => c2.HealthAsync());
        Assert.True(ApiErrors.IsPaused(ex));
        Assert.Equal("disk mismatch", ex.ApiMessage);
    }

    [Fact]
    public async Task Enforce_With_Options()
    {
        var (c, h) = TestClient(_ => Json(HttpStatusCode.OK, """{"allow":true}"""));
        var now = new DateTimeOffset(2026, 6, 15, 0, 0, 0, TimeSpan.Zero);
        var ok = await c.EnforceAsync("alice", "doc:42",
            new CallOptions().WithScenarios("VIP", "EU").WithNow(now));
        Assert.True(ok);
        Assert.Equal(HttpMethod.Post, h.LastRequest!.Method);
        Assert.Equal("/v1/enforce", h.LastRequest.RequestUri!.AbsolutePath);
        using var doc = JsonDocument.Parse(h.LastBody!);
        Assert.Equal("alice", doc.RootElement.GetProperty("subject").GetString());
        Assert.Equal("doc:42", doc.RootElement.GetProperty("target").GetString());
        Assert.Equal(2, doc.RootElement.GetProperty("scenarios").GetArrayLength());
        Assert.Equal("2026-06-15T00:00:00Z", doc.RootElement.GetProperty("now").GetString());
    }

    [Fact]
    public async Task Enforce_Omits_Empty_Options()
    {
        var (c, h) = TestClient(_ => Json(HttpStatusCode.OK, """{"allow":false}"""));
        Assert.False(await c.EnforceAsync("a", "b"));
        using var doc = JsonDocument.Parse(h.LastBody!);
        Assert.False(doc.RootElement.TryGetProperty("scenarios", out _));
        Assert.False(doc.RootElement.TryGetProperty("now", out _));
    }

    [Fact]
    public async Task Reachable_Query()
    {
        var (c, h) = TestClient(_ => Json(HttpStatusCode.OK,
            """{"subject":"alice","reachable":["alice","doc:42"]}"""));
        var got = await c.ReachableAsync("alice", new CallOptions().WithScenarios("VIP", "EU"));
        Assert.Equal(["alice", "doc:42"], got);
        Assert.Equal("subject=alice&scenario=VIP&scenario=EU", h.LastRequest!.RequestUri!.Query.TrimStart('?'));
    }

    [Fact]
    public async Task Binding_CRUD()
    {
        const string sample =
            """{"src":"alice","dst":"role:editor","scenario":"","enabled":true,"conditions":[{"kind":"ALL"}]}""";

        var (c1, _) = TestClient(_ => Json(HttpStatusCode.OK, $$"""{"bindings":[{{sample}}]}"""));
        var list = await c1.ListBindingsAsync();
        Assert.Single(list);
        Assert.Equal("alice", list[0].Src);
        Assert.Equal(ConditionKind.All, list[0].Conditions[0].Kind);

        var (c2, h2) = TestClient(_ => Json(HttpStatusCode.OK, sample));
        var b = await c2.GetBindingAsync("alice", "role:editor", "");
        Assert.Equal("role:editor", b.Dst);
        Assert.Contains("src=alice", h2.LastRequest!.RequestUri!.Query);
        Assert.Contains("dst=role%3Aeditor", h2.LastRequest.RequestUri.Query);

        var (c3, _) = TestClient(_ => Json(HttpStatusCode.NotFound, """{"error":"binding not found"}"""));
        var nf = await Assert.ThrowsAsync<ApiException>(() => c3.GetBindingAsync("x", "y", ""));
        Assert.True(ApiErrors.IsNotFound(nf));

        var (c4, h4) = TestClient(_ => Json(HttpStatusCode.Created, sample));
        var created = await c4.AddBindingAsync(new Binding
        {
            Src = "alice",
            Dst = "role:editor",
            Enabled = true,
            Conditions = [Condition.All()],
        });
        Assert.Equal("alice", created.Src);
        Assert.Equal(HttpMethod.Post, h4.LastRequest!.Method);

        var (c5, _) = TestClient(_ => Json(HttpStatusCode.Conflict, """{"error":"duplicate"}"""));
        Assert.True(ApiErrors.IsConflict(
            await Assert.ThrowsAsync<ApiException>(() => c5.AddBindingAsync(new Binding { Src = "a", Dst = "b" }))));

        var (c6, h6) = TestClient(_ => Json(HttpStatusCode.OK, sample));
        await c6.UpdateBindingAsync(new Binding { Src = "alice", Dst = "role:editor", Enabled = true });
        Assert.Equal(HttpMethod.Put, h6.LastRequest!.Method);

        var (c7, h7) = TestClient(_ => Json(HttpStatusCode.OK, """{"ok":true}"""));
        await c7.SetEnabledAsync("alice", "role:editor", "", false);
        Assert.Equal(HttpMethod.Patch, h7.LastRequest!.Method);
        Assert.Equal("/v1/bindings/enabled", h7.LastRequest.RequestUri!.AbsolutePath);
        using var body = JsonDocument.Parse(h7.LastBody!);
        Assert.False(body.RootElement.GetProperty("enabled").GetBoolean());

        var (c8, h8) = TestClient(_ => new HttpResponseMessage(HttpStatusCode.NoContent));
        await c8.RemoveBindingAsync("alice", "role:editor", "VIP");
        Assert.Equal(HttpMethod.Delete, h8.LastRequest!.Method);
        Assert.Contains("scenario=VIP", h8.LastRequest.RequestUri!.Query);
    }

    [Fact]
    public async Task Error_Mapping()
    {
        async Task Check(HttpStatusCode status, string body, Func<Exception?, bool> pred, string msg)
        {
            var (c, _) = TestClient(_ => Json(status, body));
            var ex = await Assert.ThrowsAsync<ApiException>(() => c.HealthAsync());
            Assert.True(pred(ex));
            Assert.Equal(msg, ex.ApiMessage);
            Assert.Equal((int)status, ex.StatusCode);
        }

        await Check(HttpStatusCode.BadRequest, """{"error":"resource id contains reserved char"}""",
            ApiErrors.IsBadRequest, "resource id contains reserved char");
        await Check(HttpStatusCode.NotFound, """{"error":"binding not found"}""",
            ApiErrors.IsNotFound, "binding not found");
        await Check(HttpStatusCode.Conflict, """{"error":"binding (src,dst,scenario) exists"}""",
            ApiErrors.IsConflict, "binding (src,dst,scenario) exists");
        await Check(HttpStatusCode.ServiceUnavailable, """{"error":"service paused","reason":"x"}""",
            ApiErrors.IsPaused, "service paused");

        var (c, _) = TestClient(_ => new HttpResponseMessage(HttpStatusCode.InternalServerError)
        {
            Content = new StringContent("boom", Encoding.UTF8, "text/plain"),
        });
        var ae = await Assert.ThrowsAsync<ApiException>(() => c.HealthAsync());
        Assert.Equal("", ae.ApiMessage);
        Assert.Equal("boom", Encoding.UTF8.GetString(ae.Body));
    }

    [Fact]
    public async Task Request_Headers()
    {
        var handler = new RecordingHandler(_ => Json(HttpStatusCode.OK, """{"allow":true}"""));
        using var http = new HttpClient(handler);
        using var client = new Client("http://localhost:8080",
            new ClientOptions().WithUserAgent("custom/1.0").WithHttpClient(http));
        await client.EnforceAsync("a", "b");
        Assert.Equal("custom/1.0", handler.LastRequest!.Headers.UserAgent.ToString());
        Assert.Contains("application/json", handler.LastRequest.Headers.Accept.ToString());
        Assert.Equal("application/json", handler.LastRequest.Content!.Headers.ContentType!.MediaType);
    }

    [Fact]
    public async Task Cancellation()
    {
        var (c, _) = TestClient(_ => Json(HttpStatusCode.OK, """{"allow":true}"""));
        using var cts = new CancellationTokenSource();
        await cts.CancelAsync();
        await Assert.ThrowsAnyAsync<OperationCanceledException>(() => c.EnforceAsync("a", "b", cancellationToken: cts.Token));
    }

    [Fact]
    public void Default_Timeout()
    {
        using var c = Client.Create("http://localhost:8080");
        Assert.Equal(TimeSpan.FromSeconds(30), c.HttpTimeout);

        using var c2 = new Client("http://localhost:8080",
            new ClientOptions().WithTimeout(TimeSpan.FromSeconds(5)));
        Assert.Equal(TimeSpan.FromSeconds(5), c2.HttpTimeout);
    }

    [Fact]
    public async Task JoinPath_Preserves_Base_Path()
    {
        var handler = new RecordingHandler(_ => Json(HttpStatusCode.OK, """{"status":"ok"}"""));
        using var http = new HttpClient(handler);
        using var client = new Client("http://localhost:8080/api",
            new ClientOptions().WithHttpClient(http));
        await client.HealthAsync();
        Assert.Equal("/api/healthz", handler.LastRequest!.RequestUri!.AbsolutePath);
    }

    [Fact]
    public void CACertFile_Missing()
    {
        Assert.Throws<ClientConfigException>(() =>
            new Client("https://localhost:8443",
                new ClientOptions().WithCACertFile("/nonexistent/ca.crt")));
    }

    [Fact]
    public void JoinPath_Unit()
    {
        var u = Client.JoinPath(new Uri("http://localhost:8080/prefix/"), "/v1/enforce");
        Assert.Equal("http://localhost:8080/prefix/v1/enforce", u.ToString());
    }
}

internal sealed class RecordingHandler : HttpMessageHandler
{
    private readonly Func<HttpRequestMessage, HttpResponseMessage> _respond;

    public HttpRequestMessage? LastRequest { get; private set; }
    public string? LastBody { get; private set; }

    public RecordingHandler(Func<HttpRequestMessage, HttpResponseMessage> respond) => _respond = respond;

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request, CancellationToken cancellationToken)
    {
        cancellationToken.ThrowIfCancellationRequested();
        LastRequest = request;
        if (request.Content is not null)
            LastBody = await request.Content.ReadAsStringAsync(cancellationToken);
        else
            LastBody = null;
        return _respond(request);
    }
}

public class CACertIntegrationTests : IDisposable
{
    private readonly string _tempDir;

    public CACertIntegrationTests()
    {
        _tempDir = Path.Combine(Path.GetTempPath(), $"rbac-test-{Guid.NewGuid()}");
        Directory.CreateDirectory(_tempDir);
    }

    [Fact]
    public void WithCACertPath_Validates_NonEmpty()
    {
        var ex = Assert.ThrowsAny<ArgumentException>(() =>
            new ClientOptions().WithCACertPath(""));
    }

    [Fact]
    public void WithCACertPath_Rejected_With_HTTP_BaseURL()
    {
        var caPath = Path.Combine(_tempDir, "ca.pem");
        var ex = Assert.ThrowsAny<ClientConfigException>(() =>
            new Client("http://localhost:8080", new ClientOptions().WithCACertPath(caPath)));
        Assert.Contains("TLS options", ex.Message);
    }

    [Fact]
    public void Client_WithCACertPath_Integration()
    {
        // This test verifies that the option can be set without error.
        // Full end-to-end testing would require a running server with /v1/ca-cert endpoint.
        var caPath = Path.Combine(_tempDir, "ca-integration.pem");
        
        // Should not throw when creating options
        var options = new ClientOptions().WithCACertPath(caPath);
        Assert.NotNull(options);
    }

    public void Dispose()
    {
        try
        {
            Directory.Delete(_tempDir, true);
        }
        catch
        {
            // Ignore cleanup errors
        }
    }
}
