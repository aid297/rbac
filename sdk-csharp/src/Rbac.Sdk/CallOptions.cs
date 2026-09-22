namespace Rbac;

/// <summary>Per-call options for <see cref="Client.EnforceAsync"/> and <see cref="Client.ReachableAsync"/>.</summary>
public sealed class CallOptions
{
    internal List<string> Scenarios { get; private init; } = [];
    internal DateTimeOffset? Now { get; private init; }

    /// <summary>Sets the scenario list (returns a new options instance).</summary>
    public CallOptions WithScenarios(params string[] scenarios) =>
        new() { Scenarios = [.. scenarios], Now = Now };

    /// <summary>Sets the scenario list (returns a new options instance).</summary>
    public CallOptions WithScenarios(IEnumerable<string> scenarios) =>
        new() { Scenarios = [.. scenarios], Now = Now };

    /// <summary>Sets the evaluation timestamp (Enforce only). When omitted, the service uses its own clock.</summary>
    public CallOptions WithNow(DateTimeOffset now) =>
        new() { Scenarios = Scenarios, Now = now };
}
