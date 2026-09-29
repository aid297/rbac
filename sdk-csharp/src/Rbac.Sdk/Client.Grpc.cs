using Pb = Rbac.V1;

namespace Rbac;

public sealed partial class Client
{
    private global::Grpc.Core.CallOptions MakeGrpcCallOptions(CancellationToken ct)
    {
        // Match Go / HTTP infinite: timeout <= 0 (incl. InfiniteTimeSpan) → no deadline.
        if (_timeout <= TimeSpan.Zero)
            return new global::Grpc.Core.CallOptions(cancellationToken: ct);
        return new global::Grpc.Core.CallOptions(
            deadline: DateTime.UtcNow.Add(_timeout),
            cancellationToken: ct);
    }

    private async Task GrpcHealthAsync(CancellationToken ct)
    {
        try
        {
            await _grpc!.HealthAsync(new Pb.HealthRequest(), MakeGrpcCallOptions(ct)).ResponseAsync
                .ConfigureAwait(false);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("Health", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<bool> GrpcEnforceAsync(
        string subject, string target, CallOptions? options, CancellationToken ct)
    {
        options ??= new CallOptions();
        var req = new Pb.EnforceRequest
        {
            Subject = subject,
            Target = target,
        };
        req.Scenarios.AddRange(options.Scenarios);
        if (options.Now is { } now)
            req.Now = GrpcConvert.ToTimestamp(now);
        try
        {
            var resp = await _grpc!.EnforceAsync(req, MakeGrpcCallOptions(ct)).ResponseAsync.ConfigureAwait(false);
            return resp.Allow;
        }
        catch (Exception ex) when (GrpcErrors.TryMap("Enforce", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<IReadOnlyList<string>> GrpcReachableAsync(
        string subject, CallOptions? options, CancellationToken ct)
    {
        options ??= new CallOptions();
        var req = new Pb.ReachableRequest { Subject = subject };
        req.Scenarios.AddRange(options.Scenarios);
        try
        {
            var resp = await _grpc!.ReachableAsync(req, MakeGrpcCallOptions(ct)).ResponseAsync.ConfigureAwait(false);
            return resp.Reachable.ToList();
        }
        catch (Exception ex) when (GrpcErrors.TryMap("Reachable", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<IReadOnlyList<Binding>> GrpcListBindingsAsync(CancellationToken ct)
    {
        try
        {
            var resp = await _grpc!.ListBindingsAsync(new Pb.ListBindingsRequest(), MakeGrpcCallOptions(ct))
                .ResponseAsync.ConfigureAwait(false);
            return GrpcConvert.FromProtoList(resp.Bindings);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("ListBindings", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<Binding> GrpcGetBindingAsync(
        string src, string dst, string scenario, CancellationToken ct)
    {
        try
        {
            var resp = await _grpc!.GetBindingAsync(
                new Pb.GetBindingRequest { Src = src, Dst = dst, Scenario = scenario },
                MakeGrpcCallOptions(ct)).ResponseAsync.ConfigureAwait(false);
            return GrpcConvert.FromProto(resp);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("GetBinding", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<Binding> GrpcAddBindingAsync(Binding binding, CancellationToken ct)
    {
        try
        {
            var resp = await _grpc!.AddBindingAsync(GrpcConvert.ToProto(binding), MakeGrpcCallOptions(ct))
                .ResponseAsync.ConfigureAwait(false);
            return GrpcConvert.FromProto(resp);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("AddBinding", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task<Binding> GrpcUpdateBindingAsync(Binding binding, CancellationToken ct)
    {
        try
        {
            var resp = await _grpc!.UpdateBindingAsync(GrpcConvert.ToProto(binding), MakeGrpcCallOptions(ct))
                .ResponseAsync.ConfigureAwait(false);
            return GrpcConvert.FromProto(resp);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("UpdateBinding", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task GrpcSetEnabledAsync(
        string src, string dst, string scenario, bool enabled, CancellationToken ct)
    {
        try
        {
            await _grpc!.SetEnabledAsync(
                new Pb.SetEnabledRequest
                {
                    Src = src, Dst = dst, Scenario = scenario, Enabled = enabled,
                },
                MakeGrpcCallOptions(ct)).ResponseAsync.ConfigureAwait(false);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("SetEnabled", ex, out var mapped))
        {
            throw mapped;
        }
    }

    private async Task GrpcRemoveBindingAsync(
        string src, string dst, string scenario, CancellationToken ct)
    {
        try
        {
            await _grpc!.RemoveBindingAsync(
                new Pb.RemoveBindingRequest { Src = src, Dst = dst, Scenario = scenario },
                MakeGrpcCallOptions(ct)).ResponseAsync.ConfigureAwait(false);
        }
        catch (Exception ex) when (GrpcErrors.TryMap("RemoveBinding", ex, out var mapped))
        {
            throw mapped;
        }
    }
}
