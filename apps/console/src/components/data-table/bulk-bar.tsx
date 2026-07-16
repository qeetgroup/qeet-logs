import { Button } from "@qeetrix/ui";
import type { ReactNode } from "react";

type BulkBarProps = {
  count: number;
  /** Live progress during a long-running fan-out action. */
  progress?: { done: number; total: number } | null;
  disabled?: boolean;
  onClear: () => void;
  /** Destructive/secondary bulk actions rendered at the right. */
  children: ReactNode;
};

// Strip shown between a card header and its table when one or more rows are
// selected — surfaces the selection count + bulk actions consistently.
export function BulkBar({ count, progress, disabled, onClear, children }: BulkBarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-y bg-muted/40 px-4 py-2 text-sm">
      <span className="font-medium">{count} selected</span>
      {progress && (
        <span role="status" aria-live="polite" className="text-xs text-muted-foreground">
          ({progress.done} / {progress.total} processed…)
        </span>
      )}
      <Button variant="ghost" size="sm" className="ms-auto" onClick={onClear} disabled={disabled}>
        Clear
      </Button>
      {children}
    </div>
  );
}
