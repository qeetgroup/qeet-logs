import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  EmptyState,
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  XAxis,
  YAxis,
} from "recharts";
import {
  ArrowLeftIcon,
  BarChart2Icon,
  Loader2Icon,
  PlusIcon,
  SaveIcon,
  Trash2Icon,
} from "lucide-react";
import { useCallback, useState } from "react";
import { v4 as uuidv4 } from "uuid";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/dashboards")({ component: DashboardsPage });

// ── Types ────────────────────────────────────────────────────────────────────

type PanelKind = "line" | "bar" | "table" | "stat";

type Panel = {
  id: string;
  kind: PanelKind;
  title: string;
  query: string;
};

type Dashboard = {
  id: string;
  tenant_id: string;
  name: string;
  panels: Panel[];
  created_by: string | null;
  created_at: string;
  updated_at: string;
};

// The query API always returns { columns, count, rows }.
type QueryResponse = { columns: string[]; count: number; rows: Record<string, unknown>[] };

// ── Chart configs ─────────────────────────────────────────────────────────────

const lineConfig = {
  n: { label: "Count", color: "var(--chart-1)" },
} satisfies ChartConfig;

const barConfig = {
  n: { label: "Count", color: "var(--chart-2)" },
} satisfies ChartConfig;

// ── API hooks ─────────────────────────────────────────────────────────────────

function useDashboards() {
  return useQuery({
    queryKey: ["dashboards"],
    queryFn: () => api<Dashboard[]>("/v1/admin/dashboards"),
  });
}

function usePanelData(query: string, enabled: boolean) {
  return useQuery({
    queryKey: ["panel-data", query],
    queryFn: () =>
      api<QueryResponse>("/v1/query", { query: { q: query, format: "json" } }),
    enabled,
    meta: { silent: true },
    staleTime: 60_000,
    select: (d) => d.rows,
  });
}

// ── Panel renderers ──────────────────────────────────────────────────────────

function LinePanel({ query, title }: { query: string; title: string }) {
  const { data, isLoading } = usePanelData(query, !!query);
  if (isLoading) return <PanelSkeleton />;
  const rows = (data ?? []).map((r) => ({
    ts: String(r.timestamp ?? r.ts ?? r.time ?? r.hour ?? "").slice(0, 16).replace("T", " "),
    n: Number(r.count ?? r.n ?? r.events ?? 0),
  }));
  const gradId = `grad-${title.replace(/[^a-z0-9]/gi, "")}`;
  return (
    <ChartContainer config={lineConfig} className="aspect-auto h-[180px] w-full">
      <AreaChart data={rows} margin={{ left: 0, right: 8 }}>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--color-n)" stopOpacity={0.4} />
            <stop offset="100%" stopColor="var(--color-n)" stopOpacity={0.04} />
          </linearGradient>
        </defs>
        <CartesianGrid vertical={false} strokeDasharray="3 3" />
        <XAxis dataKey="ts" tickLine={false} axisLine={false} tick={{ fontSize: 10 }} tickMargin={6} />
        <YAxis tickLine={false} axisLine={false} width={32} tick={{ fontSize: 10 }} />
        <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
        <Area
          type="monotone"
          dataKey="n"
          stroke="var(--color-n)"
          fill={`url(#${gradId})`}
          strokeWidth={2}
        />
      </AreaChart>
    </ChartContainer>
  );
}

function BarPanel({ query }: { query: string }) {
  const { data, isLoading } = usePanelData(query, !!query);
  if (isLoading) return <PanelSkeleton />;
  const rows = (data ?? []).slice(0, 10).map((r) => {
    const keys = Object.keys(r);
    const labelKey = keys.find((k) => typeof r[k] === "string") ?? keys[0] ?? "label";
    const countKey = keys.find((k) => typeof r[k] === "number") ?? keys[1] ?? "n";
    return { label: String(r[labelKey] ?? ""), n: Number(r[countKey] ?? 0) };
  });
  return (
    <ChartContainer config={barConfig} className="aspect-auto h-[180px] w-full">
      <BarChart data={rows} layout="vertical" margin={{ left: 0, right: 16 }}>
        <CartesianGrid horizontal={false} strokeDasharray="3 3" />
        <XAxis type="number" tickLine={false} axisLine={false} tick={{ fontSize: 10 }} />
        <YAxis
          dataKey="label"
          type="category"
          tickLine={false}
          axisLine={false}
          width={80}
          tick={{ fontSize: 10 }}
        />
        <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel />} />
        <Bar dataKey="n" radius={[0, 4, 4, 0]}>
          {rows.map((_, i) => (
            <Cell key={i} fill={`var(--chart-${(i % 5) + 1})`} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}

function TablePanel({ query }: { query: string }) {
  const { data, isLoading } = usePanelData(query, !!query);
  if (isLoading) return <PanelSkeleton />;
  const rows = data ?? [];
  const cols = rows.length > 0 ? Object.keys(rows[0] ?? {}) : [];
  if (rows.length === 0)
    return <p className="py-4 text-center text-sm text-muted-foreground">No results</p>;
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            {cols.map((c) => (
              <TableHead key={c} className="text-xs">
                {c}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.slice(0, 20).map((r, i) => (
            <TableRow key={i}>
              {cols.map((c) => (
                <TableCell key={c} className="max-w-[160px] truncate font-mono text-xs">
                  {r[c] === null || r[c] === undefined ? "—" : String(r[c])}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function StatPanel({ query }: { query: string }) {
  const { data, isLoading } = usePanelData(query, !!query);
  if (isLoading) return <PanelSkeleton />;
  const row = (data ?? [])[0] ?? {};
  const val =
    Object.values(row).find((v) => typeof v === "number") ??
    Object.values(row)[0] ??
    "—";
  return (
    <div className="flex h-28 items-center justify-center">
      <span className="font-heading text-5xl font-bold tabular-nums">
        {typeof val === "number" ? val.toLocaleString("en-IN") : String(val)}
      </span>
    </div>
  );
}

function PanelSkeleton() {
  return <div className="h-[180px] animate-pulse rounded bg-muted/40" />;
}

function PanelCard({ panel, onDelete }: { panel: Panel; onDelete: () => void }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">{panel.title}</CardTitle>
          <div className="flex items-center gap-1.5">
            <Badge variant="outline" className="text-[10px] capitalize">
              {panel.kind}
            </Badge>
            <Button
              variant="ghost"
              size="icon"
              className="size-6 text-muted-foreground hover:text-destructive"
              onClick={onDelete}
            >
              <Trash2Icon className="size-3.5" />
            </Button>
          </div>
        </div>
        <CardDescription className="truncate font-mono text-[10px]">{panel.query}</CardDescription>
      </CardHeader>
      <CardContent className="pt-0">
        {panel.kind === "line" && <LinePanel query={panel.query} title={panel.title} />}
        {panel.kind === "bar" && <BarPanel query={panel.query} />}
        {panel.kind === "table" && <TablePanel query={panel.query} />}
        {panel.kind === "stat" && <StatPanel query={panel.query} />}
      </CardContent>
    </Card>
  );
}

// ── Add Panel Sheet ──────────────────────────────────────────────────────────

const EXAMPLE_QUERIES: Record<PanelKind, string> = {
  line: "SELECT toString(toStartOfHour(timestamp)) as hour, count() as n FROM logs GROUP BY hour ORDER BY hour ASC LIMIT 24",
  bar: "SELECT service, count() as n FROM logs GROUP BY service ORDER BY n DESC LIMIT 10",
  table: "SELECT timestamp, service, level, body FROM logs ORDER BY timestamp DESC LIMIT 20",
  stat: "SELECT count() as n FROM logs WHERE level = 'error'",
};

function AddPanelSheet({
  open,
  onOpenChange,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onAdd: (panel: Panel) => void;
}) {
  const [kind, setKind] = useState<PanelKind>("line");
  const [errors, setErrors] = useState<Record<string, string>>({});

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const title = String(fd.get("title") ?? "").trim();
    const query = String(fd.get("query") ?? "").trim();
    const errs: Record<string, string> = {};
    if (!title) errs.title = "Title is required.";
    if (!query) errs.query = "Query is required.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    onAdd({ id: uuidv4(), kind, title, query });
    onOpenChange(false);
    setErrors({});
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>Add Panel</SheetTitle>
          <SheetDescription>
            Configure a visualisation panel for this dashboard.
          </SheetDescription>
        </SheetHeader>
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <div className="flex-1 overflow-y-auto px-4 py-3">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="panel-kind">Panel type</FieldLabel>
                <Select
                  value={kind}
                  onValueChange={(v) => v != null && setKind(v as PanelKind)}
                >
                  <SelectTrigger id="panel-kind">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="line">Line — time-series area chart</SelectItem>
                    <SelectItem value="bar">Bar — horizontal bar chart</SelectItem>
                    <SelectItem value="table">Table — raw query results</SelectItem>
                    <SelectItem value="stat">Stat — single KPI number</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="panel-title">Title</FieldLabel>
                <Input id="panel-title" name="title" placeholder="Errors in last 24h" />
                {errors.title && <FieldError>{errors.title}</FieldError>}
              </Field>
              <Field>
                <FieldLabel htmlFor="panel-query">Query</FieldLabel>
                <textarea
                  id="panel-query"
                  name="query"
                  defaultValue={EXAMPLE_QUERIES[kind]}
                  key={kind}
                  rows={6}
                  className="w-full resize-none rounded-md border bg-muted/30 p-3 font-mono text-xs focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary"
                  spellCheck={false}
                />
                {errors.query && <FieldError>{errors.query}</FieldError>}
              </Field>
            </FieldGroup>
          </div>
          <SheetFooter className="border-t px-4 py-3">
            <SheetClose render={<Button variant="outline" type="button" />}>Cancel</SheetClose>
            <Button type="submit">
              <PlusIcon className="mr-1.5 size-4" />
              Add Panel
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

// ── Create Dashboard Sheet ────────────────────────────────────────────────────

function CreateDashboardSheet({
  open,
  onOpenChange,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreate: (d: Dashboard) => void;
}) {
  const qc = useQueryClient();
  const [error, setError] = useState("");

  const createM = useMutation({
    mutationFn: (name: string) =>
      api<Dashboard>("/v1/admin/dashboards", {
        method: "POST",
        body: { name, panels: [] },
      }),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ["dashboards"] });
      onCreate(d);
      onOpenChange(false);
    },
    meta: { successMessage: "Dashboard created" },
  });

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const name = String(fd.get("name") ?? "").trim();
    if (!name) {
      setError("Name is required.");
      return;
    }
    setError("");
    createM.mutate(name);
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle>New Dashboard</SheetTitle>
          <SheetDescription>Give your dashboard a name to get started.</SheetDescription>
        </SheetHeader>
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <div className="flex-1 px-4 py-3">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="db-name">Name</FieldLabel>
                <Input
                  id="db-name"
                  name="name"
                  placeholder="Production Overview"
                  autoFocus
                />
                {error && <FieldError>{error}</FieldError>}
              </Field>
            </FieldGroup>
          </div>
          <SheetFooter className="border-t px-4 py-3">
            <SheetClose render={<Button variant="outline" type="button" />}>Cancel</SheetClose>
            <Button type="submit" disabled={createM.isPending}>
              {createM.isPending && (
                <Loader2Icon className="mr-1.5 size-4 animate-spin" />
              )}
              Create
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

// ── Dashboard builder view ────────────────────────────────────────────────────

function DashboardBuilder({
  dashboard,
  onBack,
}: {
  dashboard: Dashboard;
  onBack: () => void;
}) {
  const qc = useQueryClient();
  const [panels, setPanels] = useState<Panel[]>(() => {
    try {
      return Array.isArray(dashboard.panels) ? (dashboard.panels as Panel[]) : [];
    } catch {
      return [];
    }
  });
  const [addingPanel, setAddingPanel] = useState(false);
  const [dirty, setDirty] = useState(false);

  const saveM = useMutation({
    mutationFn: () =>
      api<Dashboard>(`/v1/admin/dashboards/${dashboard.id}`, {
        method: "PUT",
        body: { name: dashboard.name, panels },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["dashboards"] });
      setDirty(false);
    },
    meta: { successMessage: "Dashboard saved" },
  });

  const addPanel = useCallback((panel: Panel) => {
    setPanels((p) => [...p, panel]);
    setDirty(true);
  }, []);

  const removePanel = useCallback((id: string) => {
    setPanels((p) => p.filter((panel) => panel.id !== id));
    setDirty(true);
  }, []);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={onBack}>
            <ArrowLeftIcon className="size-4" />
          </Button>
          <div>
            <h1 className="font-heading text-2xl font-semibold tracking-tight">
              {dashboard.name}
            </h1>
            <p className="text-sm text-muted-foreground">
              {panels.length} panel{panels.length !== 1 ? "s" : ""} ·{" "}
              <TimeSince value={dashboard.updated_at} />
            </p>
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setAddingPanel(true)}>
            <PlusIcon className="mr-1.5 size-4" />
            Add Panel
          </Button>
          <Button
            onClick={() => saveM.mutate()}
            disabled={!dirty || saveM.isPending}
          >
            {saveM.isPending ? (
              <Loader2Icon className="mr-1.5 size-4 animate-spin" />
            ) : (
              <SaveIcon className="mr-1.5 size-4" />
            )}
            Save
          </Button>
        </div>
      </div>

      {panels.length === 0 ? (
        <Card>
          <CardContent className="py-16">
            <EmptyState
              icon={BarChart2Icon}
              title="No panels yet"
              description="Add a line, bar, table, or stat panel to visualise your log data."
            >
              <Button onClick={() => setAddingPanel(true)}>
                <PlusIcon className="mr-1.5 size-4" />
                Add first panel
              </Button>
            </EmptyState>
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {panels.map((panel) => (
            <PanelCard
              key={panel.id}
              panel={panel}
              onDelete={() => removePanel(panel.id)}
            />
          ))}
        </div>
      )}

      <AddPanelSheet
        open={addingPanel}
        onOpenChange={setAddingPanel}
        onAdd={addPanel}
      />
    </div>
  );
}

// ── Dashboard list view ───────────────────────────────────────────────────────

function DashboardsPage() {
  const dashQ = useDashboards();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<Dashboard | null>(null);

  const deleteM = useMutation({
    mutationFn: (id: string) =>
      api(`/v1/admin/dashboards/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["dashboards"] });
      if (selected) setSelected(null);
    },
    meta: { successMessage: "Dashboard deleted" },
  });

  if (selected) {
    return <DashboardBuilder dashboard={selected} onBack={() => setSelected(null)} />;
  }

  const dashboards = dashQ.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Dashboards</h1>
          <p className="text-sm text-muted-foreground">
            Custom visualisation panels over your log data
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <PlusIcon className="mr-1.5 size-4" />
          New Dashboard
        </Button>
      </div>

      {dashQ.isLoading ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-36 animate-pulse rounded-xl border bg-muted/30" />
          ))}
        </div>
      ) : dashboards.length === 0 ? (
        <Card>
          <CardContent className="py-16">
            <EmptyState
              icon={BarChart2Icon}
              title="No dashboards"
              description="Create a dashboard to pin charts and tables from your log data."
            >
              <Button onClick={() => setCreating(true)}>
                <PlusIcon className="mr-1.5 size-4" />
                Create first dashboard
              </Button>
            </EmptyState>
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {dashboards.map((d) => (
            <Card
              key={d.id}
              className="cursor-pointer transition-shadow hover:shadow-md"
              onClick={() => setSelected(d)}
            >
              <CardHeader>
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="text-base">{d.name}</CardTitle>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={(e) => {
                      e.stopPropagation();
                      deleteM.mutate(d.id);
                    }}
                    aria-label="Delete dashboard"
                  >
                    <Trash2Icon className="size-3.5" />
                  </Button>
                </div>
                <CardDescription>
                  {Array.isArray(d.panels) ? d.panels.length : 0} panel
                  {!Array.isArray(d.panels) || d.panels.length !== 1 ? "s" : ""} ·{" "}
                  <TimeSince value={d.updated_at} />
                </CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      )}

      <CreateDashboardSheet
        open={creating}
        onOpenChange={setCreating}
        onCreate={(d) => setSelected(d)}
      />
    </div>
  );
}
