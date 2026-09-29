using Google.Protobuf.WellKnownTypes;
using Pb = Rbac.V1;

namespace Rbac;

internal static class GrpcConvert
{
    /// <summary>UTC timestamp truncated to whole seconds (matches Go SDK / service wire precision).</summary>
    public static Timestamp ToTimestamp(DateTimeOffset t)
    {
        var utc = t.ToUniversalTime();
        utc = new DateTimeOffset(utc.Year, utc.Month, utc.Day, utc.Hour, utc.Minute, utc.Second, TimeSpan.Zero);
        return Timestamp.FromDateTimeOffset(utc);
    }

    public static Pb.Binding ToProto(Binding b)
    {
        var outb = new Pb.Binding
        {
            Src = b.Src ?? "",
            Dst = b.Dst ?? "",
            Scenario = b.Scenario ?? "",
        };
        if (b.Enabled is { } en)
            outb.Enabled = en;

        foreach (var c in b.Conditions)
        {
            var pc = new Pb.Condition
            {
                Kind = c.Kind switch
                {
                    ConditionKind.All => "ALL",
                    ConditionKind.Time => "TIME",
                    _ => throw new ClientConfigException($"unknown condition kind: {c.Kind}"),
                },
            };
            if (c.Kind == ConditionKind.Time)
            {
                if (c.Start is { } s)
                    pc.Start = ToTimestamp(s);
                if (c.End is { } e)
                    pc.End = ToTimestamp(e);
            }
            outb.Conditions.Add(pc);
        }
        return outb;
    }

    public static Binding FromProto(Pb.Binding? b)
    {
        if (b is null)
            return new Binding();

        var outb = new Binding
        {
            Src = b.Src,
            Dst = b.Dst,
            Scenario = b.Scenario,
        };
        if (b.HasEnabled)
            outb.Enabled = b.Enabled;

        foreach (var c in b.Conditions)
        {
            var kind = c.Kind.ToUpperInvariant() switch
            {
                "ALL" => ConditionKind.All,
                "TIME" => ConditionKind.Time,
                _ => throw new ApiException("RPC", "Binding", 400, Array.Empty<byte>(),
                    $"unknown condition kind: \"{c.Kind}\""),
            };
            DateTimeOffset? start = c.Start is null
                ? null
                : TruncateSeconds(c.Start.ToDateTimeOffset().ToUniversalTime());
            DateTimeOffset? end = c.End is null
                ? null
                : TruncateSeconds(c.End.ToDateTimeOffset().ToUniversalTime());

            outb.Conditions.Add(new Condition { Kind = kind, Start = start, End = end });
        }
        return outb;
    }

    public static List<Binding> FromProtoList(IEnumerable<Pb.Binding> list)
    {
        var outList = new List<Binding>();
        foreach (var b in list)
            outList.Add(FromProto(b));
        return outList;
    }

    private static DateTimeOffset TruncateSeconds(DateTimeOffset t) =>
        new(t.Year, t.Month, t.Day, t.Hour, t.Minute, t.Second, TimeSpan.Zero);
}
