import { describe, expect, it } from "vitest";

import {
  cell,
  formatBytes,
  formatDateTime,
  formatNumber,
  levelKind,
  relativeTime,
  severityKind,
  toCSV,
} from "./format";

describe("cell", () => {
  it("returns an empty string for null/undefined", () => {
    expect(cell(null)).toBe("");
    expect(cell(undefined)).toBe("");
  });

  it("passes strings through and stringifies primitives", () => {
    expect(cell("hello")).toBe("hello");
    expect(cell(42)).toBe("42");
    expect(cell(true)).toBe("true");
  });

  it("JSON-encodes objects", () => {
    expect(cell({ a: 1 })).toBe('{"a":1}');
  });
});

describe("formatBytes", () => {
  it("handles the empty case", () => {
    expect(formatBytes(null)).toBe("—");
    expect(formatBytes(undefined)).toBe("—");
    expect(formatBytes(Number.NaN)).toBe("—");
  });

  it("formats across units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1024 * 1024)).toBe("1.0 MB");
    expect(formatBytes(1536)).toBe("1.5 KB");
  });
});

describe("formatNumber", () => {
  it("returns a dash for empty values", () => {
    expect(formatNumber(null)).toBe("—");
    expect(formatNumber(Number.NaN)).toBe("—");
  });

  it("renders integers as strings", () => {
    expect(formatNumber(0)).toBe("0");
    expect(formatNumber(1000)).toContain("1");
  });
});

describe("relativeTime", () => {
  it("returns a dash for empty input", () => {
    expect(relativeTime(null)).toBe("—");
    expect(relativeTime("")).toBe("—");
  });

  it("passes through unparseable input", () => {
    expect(relativeTime("not-a-date")).toBe("not-a-date");
  });

  it("describes a past time", () => {
    const threeMinAgo = new Date(Date.now() - 3 * 60_000).toISOString();
    expect(relativeTime(threeMinAgo)).toContain("min");
  });
});

describe("formatDateTime", () => {
  it("returns a dash for empty input", () => {
    expect(formatDateTime(null)).toBe("—");
  });

  it("passes through unparseable input", () => {
    expect(formatDateTime("nonsense")).toBe("nonsense");
  });

  it("formats a valid ISO timestamp", () => {
    expect(formatDateTime("2026-07-16T10:00:00Z")).toContain("2026");
  });
});

describe("levelKind", () => {
  it("maps severities to visual kinds", () => {
    expect(levelKind("error")).toBe("danger");
    expect(levelKind("FATAL")).toBe("danger");
    expect(levelKind("warn")).toBe("warning");
    expect(levelKind("info")).toBe("info");
    expect(levelKind("debug")).toBe("muted");
    expect(levelKind(null)).toBe("muted");
  });
});

describe("severityKind", () => {
  it("maps incident severities to visual kinds", () => {
    expect(severityKind("critical")).toBe("danger");
    expect(severityKind("high")).toBe("danger");
    expect(severityKind("medium")).toBe("warning");
    expect(severityKind("low")).toBe("info");
    expect(severityKind("info")).toBe("muted");
  });
});

describe("toCSV", () => {
  it("builds a header row and escapes commas/quotes/newlines", () => {
    const csv = toCSV(
      ["a", "b"],
      [
        { a: "1", b: "x,y" },
        { a: 'has"quote', b: "line\nbreak" },
      ],
    );
    const lines = csv.split("\n");
    expect(lines[0]).toBe("a,b");
    expect(lines[1]).toBe('1,"x,y"');
    // Quoted fields double their inner quotes and wrap newlines.
    expect(csv).toContain('"has""quote"');
    expect(csv).toContain('"line\nbreak"');
  });
});
