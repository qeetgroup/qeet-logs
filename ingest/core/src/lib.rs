//! qeet-logs-core — shared ingest types and logic.
//!
//! - [`record`]: the canonical `LogRecord` (TAD §6.2), the lenient client
//!   `IngestInput`, and `build_record` which normalises + runs the PII gate.
//! - [`metric`]: the canonical `MetricRecord` (PRD Module 02/05) and
//!   `build_metric` (OTLP + Prometheus data points → ClickHouse `metrics` rows).
//! - [`span`]: the canonical `SpanRecord` (PRD Module 03/05) and `build_span`
//!   (OTLP spans → flat ClickHouse `traces` rows).
//! - [`pii`]: the synchronous, pre-storage PII gate (regex detectors with
//!   per-tenant mask/hash/drop actions).
//! - [`normalize`]: timestamp and log-level canonicalisation.

pub mod metric;
pub mod normalize;
pub mod pii;
pub mod record;
pub mod remap;
pub mod span;

pub use metric::{build_metric, MetricInput, MetricRecord};
pub use pii::MaskingActions;
pub use record::{build_record, IngestInput, LogRecord};
pub use remap::Program;
pub use span::{build_span, SpanInput, SpanRecord};
