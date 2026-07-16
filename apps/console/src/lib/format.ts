// Small presentation helpers shared across log-domain screens.

/** Coerce an unknown cell (from the `{rows}` envelope) into a display string. */
export function cell(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

/** Compact relative time, e.g. "3m ago" / "in 2h". */
export function relativeTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined || input === "") return "—";
  const then = new Date(input).getTime();
  if (Number.isNaN(then)) return String(input);
  const diff = then - Date.now();
  const abs = Math.abs(diff);
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ["day", 86_400_000],
    ["hour", 3_600_000],
    ["minute", 60_000],
    ["second", 1000],
  ];
  const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  for (const [unit, ms] of units) {
    if (abs >= ms || unit === "second") {
      return rtf.format(Math.round(diff / ms), unit);
    }
  }
  return "just now";
}

/** Absolute timestamp, locale-formatted, or a dash for empty. */
export function formatDateTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined || input === "") return "—";
  const d = new Date(input);
  if (Number.isNaN(d.getTime())) return String(input);
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Human-readable byte size. */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes)) return "—";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let val = bytes / 1024;
  let i = 0;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${units[i]}`;
}

/** Thousands-separated integer. */
export function formatNumber(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return "—";
  return value.toLocaleString();
}

/** Map a log level / severity to a StatusPill kind. */
export function levelKind(
  level: string | null | undefined,
): "danger" | "warning" | "info" | "muted" {
  const l = (level ?? "").toLowerCase();
  if (["error", "err", "fatal", "critical", "crit", "emergency", "alert"].includes(l))
    return "danger";
  if (["warn", "warning"].includes(l)) return "warning";
  if (["info", "notice"].includes(l)) return "info";
  return "muted";
}

/** Map an incident severity to a StatusPill kind. */
export function severityKind(
  sev: string | null | undefined,
): "danger" | "warning" | "info" | "muted" {
  const s = (sev ?? "").toLowerCase();
  if (s === "critical" || s === "high") return "danger";
  if (s === "medium") return "warning";
  if (s === "low") return "info";
  return "muted";
}

/** Trigger a browser download of a text blob (used for exports). */
export function downloadText(filename: string, contents: string, mime = "text/plain") {
  if (typeof window === "undefined") return;
  const blob = new Blob([contents], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

/** Serialize the `{columns, rows}` envelope to CSV for client-side export. */
export function toCSV(columns: string[], rows: Array<Record<string, unknown>>): string {
  const esc = (v: unknown) => {
    const s = cell(v);
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  const head = columns.map(esc).join(",");
  const body = rows.map((r) => columns.map((c) => esc(r[c])).join(",")).join("\n");
  return `${head}\n${body}`;
}
