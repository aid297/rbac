using System.Collections.Concurrent;
using Google.Protobuf.WellKnownTypes;
using Grpc.Core;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Microsoft.AspNetCore.TestHost;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Pb = Rbac.V1;
using Rbac;

namespace Rbac.Sdk.Tests;

public class GrpcClientTests
{
    [Fact]
    public void CreateGrpc_Validation()
    {
        Assert.Throws<ClientConfigException>(() => Client.CreateGrpc(""));
        Assert.Throws<ClientConfigException>(() => Client.CreateGrpc("http://localhost:9080"));
        Assert.Throws<ClientConfigException>(() =>
            Client.CreateGrpc("localhost:9080", new ClientOptions().WithHttpClient(new HttpClient())));
        Assert.Throws<ClientConfigException>(() =>
            Client.CreateGrpc("localhost:9080", new ClientOptions().WithCACertPath("/tmp/ca.crt")));
        Assert.Throws<ClientConfigException>(() =>
            Client.CreateGrpc("localhost:9443", new ClientOptions().WithCACert(ReadOnlySpan<byte>.Empty)));
    }

    [Fact]
    public async Task Health_Enforce_CRUD()
    {
        await using var host = await FakeGrpcHost.StartAsync();
        using var client = host.CreateClient();

        await client.HealthAsync();

        var created = await client.AddBindingAsync(new Binding
        {
            Src = "alice",
            Dst = "doc:42",
            Enabled = true,
            Conditions = [Condition.All()],
        });
        Assert.Equal("alice", created.Src);

        Assert.True(await client.EnforceAsync("alice", "doc:42"));

        var nodes = await client.ReachableAsync("alice", new CallOptions().WithScenarios("VIP"));
        Assert.NotEmpty(nodes);
        Assert.Equal("alice", nodes[0]);

        var got = await client.GetBindingAsync("alice", "doc:42", "");
        Assert.Equal("doc:42", got.Dst);

        var list = await client.ListBindingsAsync();
        Assert.Single(list);

        var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero);
        var end = new DateTimeOffset(2026, 7, 1, 0, 0, 0, TimeSpan.Zero);
        await client.UpdateBindingAsync(new Binding
        {
            Src = "alice",
            Dst = "doc:42",
            Enabled = true,
            Conditions = [Condition.TimeRange(start, end)],
        });

        await client.SetEnabledAsync("alice", "doc:42", "", false);
        Assert.False(await client.EnforceAsync("alice", "doc:42"));

        var conflict = await Assert.ThrowsAsync<ApiException>(() =>
            client.AddBindingAsync(new Binding { Src = "alice", Dst = "doc:42" }));
        Assert.True(ApiErrors.IsConflict(conflict));

        await client.RemoveBindingAsync("alice", "doc:42", "");
        var nf = await Assert.ThrowsAsync<ApiException>(() =>
            client.GetBindingAsync("alice", "doc:42", ""));
        Assert.True(ApiErrors.IsNotFound(nf));
    }

    [Fact]
    public async Task Paused()
    {
        await using var host = await FakeGrpcHost.StartAsync(paused: true);
        using var client = host.CreateClient();
        var ex = await Assert.ThrowsAsync<ApiException>(() => client.HealthAsync());
        Assert.True(ApiErrors.IsPaused(ex));
        Assert.True(ApiErrors.IsGrpc(ex));
    }

    [Fact]
    public void Unavailable_Not_Paused()
    {
        var err = GrpcErrors.Map("Health", new RpcException(new Status(StatusCode.Unavailable, "connection refused")));
        Assert.False(ApiErrors.IsPaused(err));
        Assert.IsType<RpcException>(err);
        Assert.True(ApiErrors.IsGrpc(err));
    }

    [Fact]
    public async Task Close_Then_Call()
    {
        await using var host = await FakeGrpcHost.StartAsync();
        var client = host.CreateClient();
        client.Dispose();
        await Assert.ThrowsAsync<ObjectDisposedException>(() => client.HealthAsync());
        await Assert.ThrowsAsync<ObjectDisposedException>(() => client.EnforceAsync("a", "b"));
        client.Dispose(); // idempotent
    }

    [Fact]
    public async Task Close_Race_With_InFlight()
    {
        await using var host = await FakeGrpcHost.StartAsync();
        var client = host.CreateClient();
        var tasks = Enumerable.Range(0, 32)
            .Select(_ => Task.Run(async () =>
            {
                try { await client.HealthAsync(); }
                catch { /* closed or in-flight */ }
            }))
            .ToList();
        tasks.Add(Task.Run(() => client.Dispose()));
        await Task.WhenAll(tasks);
        await Assert.ThrowsAsync<ObjectDisposedException>(() => client.HealthAsync());
    }

    [Fact]
    public async Task Enforce_With_Now()
    {
        await using var host = await FakeGrpcHost.StartAsync();
        using var client = host.CreateClient();
        var now = new DateTimeOffset(2026, 6, 15, 0, 0, 0, TimeSpan.Zero);
        await client.EnforceAsync("a", "b", new CallOptions().WithNow(now).WithScenarios("S"));
        Assert.NotNull(host.Service.LastEnforce);
        Assert.Equal("a", host.Service.LastEnforce!.Subject);
        Assert.Equal(["S"], host.Service.LastEnforce.Scenarios.ToArray());
        Assert.Equal(now, host.Service.LastEnforce.Now.ToDateTimeOffset());
    }

    [Fact]
    public void Map_Grpc_Error()
    {
        Assert.True(ApiErrors.IsNotFound(
            GrpcErrors.Map("GetBinding", new RpcException(new Status(StatusCode.NotFound, "missing")))));
        Assert.True(ApiErrors.IsConflict(
            GrpcErrors.Map("AddBinding", new RpcException(new Status(StatusCode.AlreadyExists, "dup")))));
        Assert.True(ApiErrors.IsPaused(
            GrpcErrors.Map("Health", new RpcException(new Status(StatusCode.FailedPrecondition, "service paused: x")))));
        Assert.True(ApiErrors.IsBadRequest(
            GrpcErrors.Map("Enforce", new RpcException(new Status(StatusCode.InvalidArgument, "bad")))));
        Assert.False(ApiErrors.IsPaused(
            GrpcErrors.Map("Health", new RpcException(new Status(StatusCode.Unavailable, "connection error")))));
    }

    [Fact]
    public void Convert_RoundTrip()
    {
        var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, 123, TimeSpan.Zero);
        var end = new DateTimeOffset(2026, 7, 1, 0, 0, 0, 456, TimeSpan.Zero);
        var b = new Binding
        {
            Src = "alice",
            Dst = "role:editor",
            Scenario = "VIP",
            Enabled = true,
            Conditions = [Condition.TimeRange(start, end)],
        };
        var proto = GrpcConvert.ToProto(b);
        Assert.Equal(0, proto.Conditions[0].Start.Nanos);
        Assert.Equal(0, proto.Conditions[0].End.Nanos);
        var back = GrpcConvert.FromProto(proto);
        Assert.Equal(b.Src, back.Src);
        Assert.Equal(b.Dst, back.Dst);
        Assert.Equal(b.Scenario, back.Scenario);
        Assert.Equal(b.Enabled, back.Enabled);
        Assert.Equal(ConditionKind.Time, back.Conditions[0].Kind);
        Assert.Equal(new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero), back.Conditions[0].Start);
        Assert.Equal(new DateTimeOffset(2026, 7, 1, 0, 0, 0, TimeSpan.Zero), back.Conditions[0].End);
    }

    [Fact]
    public async Task Timeout_NonPositive_Has_No_Immediate_Deadline()
    {
        await using var host = await FakeGrpcHost.StartAsync();
        using var infinite = host.CreateClient(Timeout.InfiniteTimeSpan);
        await infinite.HealthAsync();

        using var zero = host.CreateClient(TimeSpan.Zero);
        await zero.HealthAsync();
    }
}

/// <summary>In-process gRPC host via TestServer (avoids plaintext HTTP/2 prior-knowledge dial quirks).</summary>
internal sealed class FakeGrpcHost : IAsyncDisposable
{
    private readonly WebApplication _app;
    private readonly HttpMessageHandler _handler;
    public FakeRbacService Service { get; }

    private FakeGrpcHost(WebApplication app, HttpMessageHandler handler, FakeRbacService service)
    {
        _app = app;
        _handler = handler;
        Service = service;
    }

    public Client CreateClient()
    {
        var channel = GrpcChannel.ForAddress("http://localhost", new GrpcChannelOptions
        {
            HttpHandler = _handler,
            DisposeHttpClient = false,
        });
        return Client.CreateGrpcForTests(channel);
    }

    public Client CreateClient(TimeSpan timeout)
    {
        var channel = GrpcChannel.ForAddress("http://localhost", new GrpcChannelOptions
        {
            HttpHandler = _handler,
            DisposeHttpClient = false,
        });
        return Client.CreateGrpcForTests(channel, timeout);
    }

    public static async Task<FakeGrpcHost> StartAsync(bool paused = false)
    {
        var service = new FakeRbacService { Paused = paused };
        var builder = WebApplication.CreateBuilder();
        builder.WebHost.UseTestServer();
        builder.WebHost.ConfigureKestrel(o =>
        {
            o.ConfigureEndpointDefaults(lo => lo.Protocols = HttpProtocols.Http2);
        });
        builder.Services.AddSingleton(service);
        builder.Services.AddGrpc();
        var app = builder.Build();
        app.MapGrpcService<FakeRbacService>();
        await app.StartAsync();

        return new FakeGrpcHost(app, app.GetTestServer().CreateHandler(), service);
    }

    public async ValueTask DisposeAsync() => await _app.DisposeAsync();
}

internal sealed class FakeRbacService : Pb.RbacService.RbacServiceBase
{
    private readonly ConcurrentDictionary<string, Pb.Binding> _bindings = new();
    public bool Paused { get; set; }
    public Pb.EnforceRequest? LastEnforce { get; private set; }

    private static string Key(string src, string dst, string scenario) =>
        src + "\0" + dst + "\0" + scenario;

    public override Task<Pb.HealthResponse> Health(Pb.HealthRequest request, ServerCallContext context)
    {
        if (Paused)
            throw new RpcException(new Status(StatusCode.FailedPrecondition, "service paused: test"));
        return Task.FromResult(new Pb.HealthResponse { Status = "ok" });
    }

    public override Task<Pb.EnforceResponse> Enforce(Pb.EnforceRequest request, ServerCallContext context)
    {
        LastEnforce = request;
        if (Paused)
            throw new RpcException(new Status(StatusCode.FailedPrecondition, "service paused: test"));
        _bindings.TryGetValue(Key(request.Subject, request.Target, ""), out var b);
        var allow = b is not null && (!b.HasEnabled || b.Enabled);
        return Task.FromResult(new Pb.EnforceResponse { Allow = allow });
    }

    public override Task<Pb.ReachableResponse> Reachable(Pb.ReachableRequest request, ServerCallContext context)
    {
        var outList = new List<string> { request.Subject };
        foreach (var b in _bindings.Values)
        {
            if (b.Src == request.Subject && (!b.HasEnabled || b.Enabled))
                outList.Add(b.Dst);
        }
        return Task.FromResult(new Pb.ReachableResponse
        {
            Subject = request.Subject,
            Reachable = { outList },
        });
    }

    public override Task<Pb.ListBindingsResponse> ListBindings(Pb.ListBindingsRequest request, ServerCallContext context)
    {
        return Task.FromResult(new Pb.ListBindingsResponse
        {
            Bindings = { _bindings.Values },
        });
    }

    public override Task<Pb.Binding> GetBinding(Pb.GetBindingRequest request, ServerCallContext context)
    {
        if (!_bindings.TryGetValue(Key(request.Src, request.Dst, request.Scenario), out var b))
            throw new RpcException(new Status(StatusCode.NotFound, "binding not found"));
        return Task.FromResult(b);
    }

    public override Task<Pb.Binding> AddBinding(Pb.Binding request, ServerCallContext context)
    {
        var k = Key(request.Src, request.Dst, request.Scenario);
        var cp = Clone(request);
        if (!cp.HasEnabled)
            cp.Enabled = true;
        if (cp.Conditions.Count == 0)
            cp.Conditions.Add(new Pb.Condition { Kind = "ALL" });
        if (!_bindings.TryAdd(k, cp))
            throw new RpcException(new Status(StatusCode.AlreadyExists, "duplicate"));
        return Task.FromResult(cp);
    }

    public override Task<Pb.Binding> UpdateBinding(Pb.Binding request, ServerCallContext context)
    {
        var k = Key(request.Src, request.Dst, request.Scenario);
        if (!_bindings.ContainsKey(k))
            throw new RpcException(new Status(StatusCode.NotFound, "binding not found"));
        var cp = Clone(request);
        _bindings[k] = cp;
        return Task.FromResult(cp);
    }

    public override Task<Pb.SetEnabledResponse> SetEnabled(Pb.SetEnabledRequest request, ServerCallContext context)
    {
        if (!_bindings.TryGetValue(Key(request.Src, request.Dst, request.Scenario), out var b))
            throw new RpcException(new Status(StatusCode.NotFound, "binding not found"));
        b.Enabled = request.Enabled;
        return Task.FromResult(new Pb.SetEnabledResponse { Ok = true });
    }

    public override Task<Pb.RemoveBindingResponse> RemoveBinding(Pb.RemoveBindingRequest request, ServerCallContext context)
    {
        if (!_bindings.TryRemove(Key(request.Src, request.Dst, request.Scenario), out _))
            throw new RpcException(new Status(StatusCode.NotFound, "binding not found"));
        return Task.FromResult(new Pb.RemoveBindingResponse());
    }

    private static Pb.Binding Clone(Pb.Binding b)
    {
        var cp = new Pb.Binding
        {
            Src = b.Src,
            Dst = b.Dst,
            Scenario = b.Scenario,
        };
        if (b.HasEnabled)
            cp.Enabled = b.Enabled;
        foreach (var c in b.Conditions)
        {
            var nc = new Pb.Condition { Kind = c.Kind };
            if (c.Start is not null)
                nc.Start = Timestamp.FromDateTimeOffset(c.Start.ToDateTimeOffset());
            if (c.End is not null)
                nc.End = Timestamp.FromDateTimeOffset(c.End.ToDateTimeOffset());
            cp.Conditions.Add(nc);
        }
        return cp;
    }
}
