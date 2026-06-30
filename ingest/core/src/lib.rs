//! qeet-logs-core — shared ingest types and logic.
//!
//! - [`record`]: the canonical `LogRecord` (TAD §6.2), the lenient client
//!   `IngestInput`, and `build_record` which normalises + runs the PII gate.
//! - [`pii`]: the synchronous, pre-storage PII gate (regex detectors with
//!   per-tenant mask/hash/drop actions).
//! - [`normalize`]: timestamp and log-level canonicalisation.

pub mod normalize;
pub mod pii;
pub mod record;

pub use pii::MaskingActions;
pub use record::{build_record, IngestInput, LogRecord};
