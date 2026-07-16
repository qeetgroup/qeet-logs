import {
  Button,
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@qeetrix/ui";
import {
  DownloadIcon,
  Rows2Icon,
  Rows3Icon,
  SearchIcon,
  SlidersHorizontalIcon,
  XIcon,
} from "lucide-react";
import type { ReactNode } from "react";

import type { Density } from "@/lib/list-view";

export type FacetFilter = {
  id: string;
  label: string;
  value: string;
  options: { label: string; value: string }[];
  onChange: (value: string) => void;
};

export type ToggleColumn = { id: string; label: string };

type ListToolbarProps = {
  search: string;
  onSearchChange: (value: string) => void;
  searchPlaceholder?: string;
  filters?: FacetFilter[];
  columns?: ToggleColumn[];
  isColumnVisible?: (id: string) => boolean;
  onToggleColumn?: (id: string) => void;
  density?: Density;
  onDensityChange?: (density: Density) => void;
  onExport?: (format: "csv" | "json") => void;
  exportDisabled?: boolean;
  hasActiveFilters?: boolean;
  onClear?: () => void;
  /** Primary actions (e.g. a "New …" button) rendered at the far right. */
  children?: ReactNode;
};

// Shared toolbar for every list page: search + faceted filters on the left,
// view controls (density, column visibility), export, and page actions on
// the right. Each control is opt-in — omit a prop and its control hides.
export function ListToolbar({
  search,
  onSearchChange,
  searchPlaceholder = "Search…",
  filters,
  columns,
  isColumnVisible,
  onToggleColumn,
  density,
  onDensityChange,
  onExport,
  exportDisabled,
  hasActiveFilters,
  onClear,
  children,
}: ListToolbarProps) {
  return (
    <div className="flex flex-col gap-2 border-b p-3 sm:flex-row sm:items-center">
      <div className="flex flex-1 flex-wrap items-center gap-2">
        <div className="relative w-full sm:max-w-xs">
          <SearchIcon className="pointer-events-none absolute inset-s-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={searchPlaceholder}
            className="ps-9"
            aria-label="Search"
          />
        </div>

        {filters?.map((f) => (
          <Select key={f.id} value={f.value || "all"} onValueChange={(v) => v && f.onChange(v)}>
            <SelectTrigger size="sm" className="w-auto min-w-32">
              <SelectValue placeholder={f.label} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{f.label}: All</SelectItem>
              {f.options.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ))}

        {hasActiveFilters && onClear && (
          <Button variant="ghost" size="sm" onClick={onClear}>
            <XIcon /> Clear
          </Button>
        )}
      </div>

      <div className="flex items-center gap-1.5">
        {density && onDensityChange && (
          <Button
            variant="outline"
            size="icon"
            aria-label={density === "compact" ? "Comfortable rows" : "Compact rows"}
            title={density === "compact" ? "Comfortable rows" : "Compact rows"}
            onClick={() => onDensityChange(density === "compact" ? "comfortable" : "compact")}
          >
            {density === "compact" ? <Rows3Icon /> : <Rows2Icon />}
          </Button>
        )}

        {columns?.length && isColumnVisible && onToggleColumn ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="icon" aria-label="Toggle columns" title="Columns">
                  <SlidersHorizontalIcon />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="min-w-40">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Columns</DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              {columns.map((c) => (
                <DropdownMenuCheckboxItem
                  key={c.id}
                  checked={isColumnVisible(c.id)}
                  onCheckedChange={() => onToggleColumn(c.id)}
                >
                  {c.label}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}

        {onExport && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="sm" disabled={exportDisabled}>
                  <DownloadIcon /> Export
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="min-w-36">
              <DropdownMenuItem onClick={() => onExport("csv")}>Download as CSV</DropdownMenuItem>
              <DropdownMenuItem onClick={() => onExport("json")}>Download as JSON</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}

        {children}
      </div>
    </div>
  );
}
