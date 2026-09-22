//! Wire types for bindings and conditions.

use chrono::{DateTime, SecondsFormat, Utc};
use serde::de::{self, Deserializer};
use serde::ser::Serializer;
use serde::{Deserialize, Serialize};

/// Condition family. Same-kind instances OR; different kinds AND.
/// `ALL` is exclusive and cannot be mixed with others on the server.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConditionKind {
    /// Unconditional always-true.
    All,
    /// Half-open time window `[start, end)`.
    Time,
}

impl ConditionKind {
    fn as_str(&self) -> &'static str {
        match self {
            ConditionKind::All => "ALL",
            ConditionKind::Time => "TIME",
        }
    }

    fn parse(s: &str) -> Result<Self, String> {
        match s {
            "ALL" => Ok(ConditionKind::All),
            "TIME" => Ok(ConditionKind::Time),
            other => Err(format!("unknown condition kind: {other:?}")),
        }
    }
}

/// Directed edge `src → dst`, matching the service `/v1` DTO.
///
/// When [`Binding::enabled`] is `None`, the field is omitted from JSON and the
/// service defaults to `true`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Binding {
    /// Source node id.
    pub src: String,
    /// Destination node id.
    pub dst: String,
    /// Optional scenario key (empty string = general edge).
    #[serde(default)]
    pub scenario: String,
    /// Enable flag; `None` omits the field (server default: true).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,
    /// Predicates on this edge.
    #[serde(default)]
    pub conditions: Vec<Condition>,
}

impl Binding {
    /// Builds a binding with `enabled` left unset (server defaults to true).
    pub fn new(src: impl Into<String>, dst: impl Into<String>) -> Self {
        Self {
            src: src.into(),
            dst: dst.into(),
            scenario: String::new(),
            enabled: None,
            conditions: Vec::new(),
        }
    }

    /// Sets the scenario.
    pub fn with_scenario(mut self, scenario: impl Into<String>) -> Self {
        self.scenario = scenario.into();
        self
    }

    /// Sets enabled explicitly.
    pub fn with_enabled(mut self, enabled: bool) -> Self {
        self.enabled = Some(enabled);
        self
    }

    /// Replaces conditions.
    pub fn with_conditions(mut self, conditions: Vec<Condition>) -> Self {
        self.conditions = conditions;
        self
    }
}

/// Predicate attached to a binding.
///
/// For [`ConditionKind::All`], `start` / `end` are ignored.
/// For [`ConditionKind::Time`], the interval is half-open `[start, end)`;
/// a missing bound means unbounded on that side.
///
/// The service stores time at RFC3339 **second** precision.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Condition {
    /// Condition family.
    pub kind: ConditionKind,
    /// Inclusive start (TIME only).
    pub start: Option<DateTime<Utc>>,
    /// Exclusive end (TIME only).
    pub end: Option<DateTime<Utc>>,
}

/// Unconditional always-true condition.
pub fn all_condition() -> Condition {
    Condition {
        kind: ConditionKind::All,
        start: None,
        end: None,
    }
}

/// TIME condition for half-open `[start, end)`. Either side may be `None`.
pub fn time_range(start: Option<DateTime<Utc>>, end: Option<DateTime<Utc>>) -> Condition {
    Condition {
        kind: ConditionKind::Time,
        start,
        end,
    }
}

#[derive(Serialize, Deserialize)]
struct ConditionWire {
    kind: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    start: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    end: Option<String>,
}

impl Serialize for Condition {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        let mut w = ConditionWire {
            kind: self.kind.as_str().to_string(),
            start: None,
            end: None,
        };
        if self.kind == ConditionKind::Time {
            if let Some(t) = self.start {
                w.start = Some(t.to_rfc3339_opts(SecondsFormat::Secs, true));
            }
            if let Some(t) = self.end {
                w.end = Some(t.to_rfc3339_opts(SecondsFormat::Secs, true));
            }
        }
        w.serialize(serializer)
    }
}

impl<'de> Deserialize<'de> for Condition {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let w = ConditionWire::deserialize(deserializer)?;
        let kind = ConditionKind::parse(&w.kind).map_err(de::Error::custom)?;
        let start = match w.start {
            Some(s) if !s.is_empty() => Some(
                DateTime::parse_from_rfc3339(&s)
                    .map_err(de::Error::custom)?
                    .with_timezone(&Utc),
            ),
            _ => None,
        };
        let end = match w.end {
            Some(s) if !s.is_empty() => Some(
                DateTime::parse_from_rfc3339(&s)
                    .map_err(de::Error::custom)?
                    .with_timezone(&Utc),
            ),
            _ => None,
        };
        Ok(Condition { kind, start, end })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn condition_marshal_shapes() {
        let all = serde_json::to_string(&all_condition()).unwrap();
        assert_eq!(all, r#"{"kind":"ALL"}"#);

        let start = Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
        let end = Utc.with_ymd_and_hms(2026, 7, 1, 0, 0, 0).unwrap();
        let both = serde_json::to_string(&time_range(Some(start), Some(end))).unwrap();
        assert_eq!(
            both,
            r#"{"kind":"TIME","start":"2026-06-01T00:00:00Z","end":"2026-07-01T00:00:00Z"}"#
        );

        let start_only = serde_json::to_string(&time_range(Some(start), None)).unwrap();
        assert_eq!(start_only, r#"{"kind":"TIME","start":"2026-06-01T00:00:00Z"}"#);
    }

    #[test]
    fn condition_rejects_unknown_kind() {
        let err = serde_json::from_str::<Condition>(r#"{"kind":"BOGUS"}"#).unwrap_err();
        assert!(err.to_string().contains("unknown condition kind"));
    }

    #[test]
    fn binding_omits_enabled_when_none() {
        let b = Binding::new("a", "b");
        let v: serde_json::Value = serde_json::to_value(&b).unwrap();
        assert!(v.get("enabled").is_none());

        let b2 = Binding::new("a", "b").with_enabled(false);
        let v2: serde_json::Value = serde_json::to_value(&b2).unwrap();
        assert_eq!(v2["enabled"], false);
    }

    #[test]
    fn binding_round_trip() {
        let start = Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
        let b = Binding::new("alice", "role:editor")
            .with_scenario("VIP")
            .with_enabled(true)
            .with_conditions(vec![time_range(Some(start), None)]);
        let data = serde_json::to_vec(&b).unwrap();
        let back: Binding = serde_json::from_slice(&data).unwrap();
        assert_eq!(back.src, "alice");
        assert_eq!(back.enabled, Some(true));
        assert_eq!(back.conditions[0].start, Some(start));
    }
}
