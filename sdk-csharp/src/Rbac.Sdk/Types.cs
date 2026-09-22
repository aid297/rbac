using System.Globalization;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Rbac;

/// <summary>Condition family. Same-kind instances OR; different kinds AND. ALL is exclusive.</summary>
public enum ConditionKind
{
    /// <summary>Unconditional always-true.</summary>
    All,

    /// <summary>Half-open time window [Start, End).</summary>
    Time,
}

/// <summary>
/// Directed edge Src → Dst, matching the service /v1 DTO.
/// When <see cref="Enabled"/> is null, the field is omitted from JSON and the service defaults to true.
/// </summary>
public sealed class Binding
{
    [JsonPropertyName("src")]
    public string Src { get; set; } = "";

    [JsonPropertyName("dst")]
    public string Dst { get; set; } = "";

    [JsonPropertyName("scenario")]
    public string Scenario { get; set; } = "";

    /// <summary>Null omits the field (server default: true).</summary>
    [JsonPropertyName("enabled")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public bool? Enabled { get; set; }

    [JsonPropertyName("conditions")]
    public List<Condition> Conditions { get; set; } = [];
}

/// <summary>
/// Predicate attached to a binding.
/// For <see cref="ConditionKind.All"/>, Start/End are ignored.
/// For <see cref="ConditionKind.Time"/>, the interval is half-open [Start, End); null means unbounded.
/// The service stores time at RFC3339 second precision.
/// </summary>
[JsonConverter(typeof(ConditionJsonConverter))]
public sealed class Condition
{
    public ConditionKind Kind { get; set; }

    public DateTimeOffset? Start { get; set; }

    public DateTimeOffset? End { get; set; }

    /// <summary>Unconditional always-true condition.</summary>
    public static Condition All() => new() { Kind = ConditionKind.All };

    /// <summary>TIME condition for half-open [start, end). Either bound may be null.</summary>
    public static Condition TimeRange(DateTimeOffset? start, DateTimeOffset? end) =>
        new() { Kind = ConditionKind.Time, Start = start, End = end };
}

internal sealed class ConditionJsonConverter : JsonConverter<Condition>
{
    private sealed class Wire
    {
        [JsonPropertyName("kind")]
        public string Kind { get; set; } = "";

        [JsonPropertyName("start")]
        [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
        public string? Start { get; set; }

        [JsonPropertyName("end")]
        [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
        public string? End { get; set; }
    }

    public override Condition Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options)
    {
        var wire = JsonSerializer.Deserialize<Wire>(ref reader, options)
            ?? throw new JsonException("null condition");
        var kind = wire.Kind switch
        {
            "ALL" => ConditionKind.All,
            "TIME" => ConditionKind.Time,
            _ => throw new JsonException($"unknown condition kind: \"{wire.Kind}\""),
        };
        return new Condition
        {
            Kind = kind,
            Start = ParseTime(wire.Start),
            End = ParseTime(wire.End),
        };
    }

    public override void Write(Utf8JsonWriter writer, Condition value, JsonSerializerOptions options)
    {
        var wire = new Wire
        {
            Kind = value.Kind switch
            {
                ConditionKind.All => "ALL",
                ConditionKind.Time => "TIME",
                _ => throw new JsonException($"unknown condition kind: {value.Kind}"),
            },
        };
        if (value.Kind == ConditionKind.Time)
        {
            if (value.Start is { } s)
                wire.Start = FormatTime(s);
            if (value.End is { } e)
                wire.End = FormatTime(e);
        }
        JsonSerializer.Serialize(writer, wire, options);
    }

    private static DateTimeOffset? ParseTime(string? s)
    {
        if (string.IsNullOrEmpty(s))
            return null;
        return DateTimeOffset.Parse(s, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind);
    }

    private static string FormatTime(DateTimeOffset t) =>
        t.UtcDateTime.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", CultureInfo.InvariantCulture);
}
