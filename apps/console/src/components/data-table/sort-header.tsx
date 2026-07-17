import { cn, TableHead } from "@qeetrix/ui";
import { ArrowDownIcon, ArrowUpIcon, ChevronsUpDownIcon } from "lucide-react";
import type * as React from "react";

import type { SortState } from "@/lib/list-view";

type SortHeaderProps = {
  columnKey: string;
  sort: SortState;
  onToggle: (key: string) => void;
  children: React.ReactNode;
  className?: string;
};

// A TableHead whose label is a button that cycles asc → desc → none and
// shows the active direction. Pairs with useListView's sort state.
export function SortHeader({ columnKey, sort, onToggle, children, className }: SortHeaderProps) {
  const active = sort?.key === columnKey;
  // Expose the current sort direction to assistive tech on the column header.
  const ariaSort = active ? (sort?.dir === "asc" ? "ascending" : "descending") : "none";
  return (
    <TableHead className={className} aria-sort={ariaSort}>
      <button
        type="button"
        onClick={() => onToggle(columnKey)}
        className="-mx-1 inline-flex items-center gap-1 rounded px-1 py-0.5 text-xs font-medium uppercase tracking-wide hover:text-foreground"
      >
        {children}
        {active ? (
          sort?.dir === "asc" ? (
            <ArrowUpIcon className="size-3.5" />
          ) : (
            <ArrowDownIcon className="size-3.5" />
          )
        ) : (
          <ChevronsUpDownIcon className={cn("size-3.5 opacity-40")} />
        )}
      </button>
    </TableHead>
  );
}
