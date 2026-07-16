import {
  DataState,
  ScrollArea,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@qeetrix/ui";
import { DatabaseIcon } from "lucide-react";

import type { QueryEnvelope } from "@/lib/api";
import { cell } from "@/lib/format";

type ResultsTableProps = {
  data?: QueryEnvelope;
  isLoading?: boolean;
  isError?: boolean;
  error?: unknown;
  emptyTitle?: string;
  emptyDescription?: string;
  /** Columns rendered in a monospace box (log body / raw JSON). */
  monoColumns?: string[];
  maxHeight?: string;
};

/**
 * Generic renderer for the `{columns, count, rows}` query envelope. Columns are
 * driven entirely by the response, so it works for any LogQL++ result shape
 * (raw log lines, aggregates, metric series).
 */
export function ResultsTable({
  data,
  isLoading,
  isError,
  error,
  emptyTitle = "No results",
  emptyDescription = "This query returned no rows for the selected tenant and time range.",
  monoColumns = ["body", "message", "raw", "line"],
  maxHeight = "60vh",
}: ResultsTableProps) {
  const columns = data?.columns ?? [];
  const rows = data?.rows ?? [];
  const mono = new Set(monoColumns);

  return (
    <DataState
      isLoading={isLoading}
      isError={isError}
      error={error}
      isEmpty={!isLoading && !isError && rows.length === 0}
      emptyIcon={DatabaseIcon}
      emptyTitle={emptyTitle}
      emptyDescription={emptyDescription}
      skeletonRows={8}
    >
      <ScrollArea className="w-full rounded-md border" style={{ maxHeight }}>
        <Table>
          <TableHeader className="sticky top-0 z-10 bg-background">
            <TableRow>
              {columns.map((c) => (
                <TableHead key={c} className="whitespace-nowrap">
                  {c}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: query rows have no stable id
              <TableRow key={i}>
                {columns.map((c) => (
                  <TableCell
                    key={c}
                    className={
                      mono.has(c)
                        ? "font-mono-logs max-w-xl truncate text-xs"
                        : "whitespace-nowrap text-sm"
                    }
                    title={cell(row[c])}
                  >
                    {cell(row[c])}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </ScrollArea>
    </DataState>
  );
}
