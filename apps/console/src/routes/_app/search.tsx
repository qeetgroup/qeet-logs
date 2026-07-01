import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { DownloadIcon, Loader2Icon, PlayIcon, SearchIcon } from "lucide-react";
import { useRef, useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/search")({ component: SearchPage });

type LogRow = Record<string, string | number | boolean | null>;

type Format = "json" | "csv" | "ndjson";

const LEVEL_COLORS: Record<string, string> = {
  error: "destructive",
  warn: "outline",
  warning: "outline",
  info: "secondary",
  debug: "secondary",
  trace: "secondary",
};

const EXAMPLE_QUERIES = [
  "SELECT * FROM logs LIMIT 20",
  "SELECT * FROM logs WHERE level = 'error' LIMIT 50",
  "SELECT service, count() as n FROM logs GROUP BY service ORDER BY n DESC",
  "SEARCH 'timeout' FROM logs LIMIT 50",
  "SELECT * FROM logs WHERE service = 'payments' ORDER BY timestamp DESC LIMIT 20",
];

function levelVariant(level: string) {
  return (LEVEL_COLORS[level?.toLowerCase()] ?? "secondary") as
    | "destructive"
    | "outline"
    | "secondary";
}

function SearchPage() {
  const [query, setQuery] = useState("SELECT * FROM logs LIMIT 20");
  const [format, setFormat] = useState<Format>("json");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const runMutation = useMutation({
    mutationFn: (q: string) =>
      api<LogRow[]>("/v1/query", { query: { q, format: "json" } }),
    meta: { silent: true },
  });

  function handleRun() {
    runMutation.mutate(query.trim());
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      handleRun();
    }
  }

  function handleExport() {
    const key = typeof window !== "undefined"
      ? window.localStorage.getItem("qeet-logs.api-key") ?? ""
      : "";
    const url = new URL("/v1/query", "http://localhost:8100");
    url.searchParams.set("q", query.trim());
    url.searchParams.set("format", format);
    const a = document.createElement("a");
    a.href = url.toString();
    a.download = `logs.${format}`;
    // Cannot set custom header via <a>, so copy URL for download
    navigator.clipboard?.writeText(url.toString());
    window.open(url.toString() + `#key=${key}`, "_blank");
  }

  const rows = runMutation.data ?? [];
  const columns = rows.length > 0 ? Object.keys(rows[0] ?? {}) : [];

  const PRIORITY_COLS = ["timestamp", "level", "service", "message"];
  const sortedCols = [
    ...PRIORITY_COLS.filter((c) => columns.includes(c)),
    ...columns.filter((c) => !PRIORITY_COLS.includes(c)),
  ];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div>
        <h1 className="font-heading text-2xl font-semibold tracking-tight">Log Search</h1>
        <p className="text-sm text-muted-foreground">Query logs with LogQL++</p>
      </div>

      {/* Query editor */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Query</CardTitle>
            <div className="flex items-center gap-2">
              <Select value={format} onValueChange={(v) => setFormat(v as Format)}>
                <SelectTrigger size="sm" className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="json">JSON</SelectItem>
                  <SelectItem value="csv">CSV</SelectItem>
                  <SelectItem value="ndjson">NDJSON</SelectItem>
                </SelectContent>
              </Select>
              <Button
                size="sm"
                variant="outline"
                onClick={handleExport}
                disabled={rows.length === 0}
              >
                <DownloadIcon className="mr-1.5 size-3.5" />
                Export
              </Button>
              <Button
                size="sm"
                onClick={handleRun}
                disabled={runMutation.isPending}
              >
                {runMutation.isPending ? (
                  <Loader2Icon className="mr-1.5 size-3.5 animate-spin" />
                ) : (
                  <PlayIcon className="mr-1.5 size-3.5" />
                )}
                Run
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="pt-0">
          <textarea
            ref={textareaRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            className="w-full resize-none rounded-md border bg-muted/30 p-3 font-mono text-sm focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary"
            rows={4}
            spellCheck={false}
            placeholder="SELECT * FROM logs LIMIT 20"
            aria-label="LogQL++ query"
          />
          <p className="mt-1.5 text-xs text-muted-foreground">
            Press <kbd className="rounded border bg-muted px-1 font-mono text-[10px]">⌘ Enter</kbd> to run.{" "}
            Supports <code className="text-[10px]">SELECT</code>, <code className="text-[10px]">SEARCH</code>, <code className="text-[10px]">TAIL</code>.
          </p>
        </CardContent>
      </Card>

      {/* Example queries */}
      <div className="flex flex-wrap gap-2">
        <span className="text-xs text-muted-foreground self-center">Examples:</span>
        {EXAMPLE_QUERIES.map((q) => (
          <button
            key={q}
            type="button"
            onClick={() => setQuery(q)}
            className="rounded-full border bg-muted/40 px-3 py-1 text-xs font-mono hover:bg-muted transition-colors text-left"
          >
            {q.length > 50 ? q.slice(0, 48) + "…" : q}
          </button>
        ))}
      </div>

      {/* Results */}
      {runMutation.isError && (
        <Card className="border-destructive/50">
          <CardContent className="py-4 text-sm text-destructive">
            {runMutation.error instanceof Error
              ? runMutation.error.message
              : "Query failed"}
          </CardContent>
        </Card>
      )}

      {!runMutation.isError && (
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <CardTitle className="text-base">
                Results{" "}
                {rows.length > 0 && (
                  <span className="text-sm font-normal text-muted-foreground">
                    ({rows.length.toLocaleString("en-IN")} rows)
                  </span>
                )}
              </CardTitle>
              {runMutation.isPending && (
                <Loader2Icon className="size-4 animate-spin text-muted-foreground" />
              )}
            </div>
          </CardHeader>
          <CardContent className="p-0">
            {!runMutation.isSuccess ? (
              <div className="px-6 py-12">
                <EmptyState
                  icon={SearchIcon}
                  title="Run a query"
                  description="Write a LogQL++ query above and press Run to see results."
                />
              </div>
            ) : rows.length === 0 ? (
              <div className="px-6 py-12">
                <EmptyState
                  icon={SearchIcon}
                  title="No results"
                  description="No log events matched the query."
                />
              </div>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      {sortedCols.map((col) => (
                        <TableHead key={col} className="whitespace-nowrap text-xs">
                          {col}
                        </TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((row, i) => (
                      <TableRow key={i} className="font-mono text-xs">
                        {sortedCols.map((col) => {
                          const val = row[col];
                          if (col === "level") {
                            return (
                              <TableCell key={col}>
                                <Badge variant={levelVariant(String(val ?? ""))} className="uppercase text-[10px]">
                                  {String(val ?? "—")}
                                </Badge>
                              </TableCell>
                            );
                          }
                          if (col === "timestamp") {
                            return (
                              <TableCell key={col} className="whitespace-nowrap text-muted-foreground">
                                <TimeSince value={String(val ?? "")} />
                              </TableCell>
                            );
                          }
                          const strVal = val === null || val === undefined ? "—" : String(val);
                          return (
                            <TableCell key={col} className="max-w-xs truncate">
                              {strVal}
                            </TableCell>
                          );
                        })}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
