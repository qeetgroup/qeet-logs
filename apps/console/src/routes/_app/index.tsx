import {
  Badge,
  Button,
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  EmptyState,
  PresenceIndicator,
  Sparkline,
  statDeltaVariants,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
  ActivityIcon,
  AlertTriangleIcon,
  ArrowDownRightIcon,
  ArrowUpRightIcon,
  CheckCircle2Icon,
  ChevronRightIcon,
  DatabaseIcon,
  ListFilterIcon,
  RadioIcon,
  ServerIcon,
  XCircleIcon,
} from "lucide-react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, XAxis, YAxis } from "recharts";

import { api } from "@/lib/api";
import type { ReadyzResponse } from "@/lib/auth";

export const Route = createFileRoute("/_app/")({ component: OverviewPage });

// ── types ────────────────────────────────────────────────────────────────────

type LogRow = Record<string, string | number>;

// The query API always returns { columns, count, rows }.
type QueryResponse = { columns: string[]; count: number; rows: LogRow[] };

type ServiceVolume = { service: string; count: number };

type HourlyVolume = { hour: string; count: number; errors: number };

// ── queries ───────────────────────────────────────────────────────────────────

function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: () => api<ReadyzResponse>("/readyz"),
    refetchInterval: 15_000,
    meta: { silent: true },
  });
}

function useServiceVolume() {
  return useQuery({
    queryKey: ["service-volume"],
    queryFn: () =>
      api<QueryResponse>("/v1/query", {
        query: {
          q: "SELECT service, count() as count FROM logs GROUP BY service ORDER BY count DESC LIMIT 8",
          format: "json",
        },
      }),
    refetchInterval: 30_000,
    meta: { silent: true },
    select: (d) => d.rows,
  });
}

function useHourlyVolume() {
  return useQuery({
    queryKey: ["hourly-volume"],
    queryFn: () =>
      api<QueryResponse>("/v1/query", {
        query: {
          q: "SELECT toString(toStartOfHour(timestamp)) as hour, count() as count FROM logs GROUP BY hour ORDER BY hour ASC LIMIT 24",
          format: "json",
        },
      }),
    refetchInterval: 60_000,
    meta: { silent: true },
    select: (d) => d.rows,
  });
}

function useErrorCount() {
  return useQuery({
    queryKey: ["error-count"],
    queryFn: () =>
      api<QueryResponse>("/v1/query", {
        query: {
          q: "SELECT count() as count FROM logs WHERE level = 'error'",
          format: "json",
        },
      }),
    refetchInterval: 30_000,
    meta: { silent: true },
    select: (d) => d.rows,
  });
}

// ── chart configs ─────────────────────────────────────────────────────────────

const volumeConfig = {
  count: { label: "Logs", color: "var(--chart-1)" },
  errors: { label: "Errors", color: "var(--chart-5)" },
} satisfies ChartConfig;

const serviceConfig = {
  count: { label: "Log events", color: "var(--chart-2)" },
} satisfies ChartConfig;

// ── components ────────────────────────────────────────────────────────────────

const cardLift =
  "motion-safe:transition-transform motion-safe:duration-200 motion-safe:hover:-translate-y-0.5";

type StatCardProps = {
  icon: React.ReactNode;
  iconClass?: string;
  title: string;
  value: string;
  delta?: string;
  positive?: boolean;
  sparkData?: number[];
};

function StatCard({ icon, iconClass = "bg-primary/10 text-primary", title, value, delta, positive, sparkData }: StatCardProps) {
  return (
    <Card className={`overflow-hidden ${cardLift}`}>
      <CardHeader className="flex flex-row items-center justify-between gap-2 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        <div className={`grid size-9 shrink-0 place-items-center rounded-lg [&_svg]:size-4 ${iconClass}`}>
          {icon}
        </div>
      </CardHeader>
      <CardContent className="pb-2">
        <div className="text-2xl font-semibold tabular-nums">{value}</div>
        {delta !== undefined && (
          <div className="mt-0.5 flex items-center gap-2">
            <span className={statDeltaVariants({ trend: positive ? "up" : "down" })}>
              {positive ? <ArrowUpRightIcon className="size-3.5" /> : <ArrowDownRightIcon className="size-3.5" />}
              {delta}
            </span>
            <span className="text-xs text-muted-foreground">vs last period</span>
          </div>
        )}
      </CardContent>
      {sparkData && (
        <div className="border-t border-border/40 text-primary">
          <Sparkline data={sparkData} type="area" height={48} className="w-full" />
        </div>
      )}
    </Card>
  );
}

function StatSkeleton() {
  return (
    <Card className="overflow-hidden">
      <CardHeader className="flex flex-row items-center justify-between gap-2 pb-2">
        <div className="h-3 w-24 rounded bg-muted" />
        <div className="size-4 rounded bg-muted" />
      </CardHeader>
      <CardContent className="pb-2">
        <div className="h-6 w-20 animate-pulse rounded bg-muted" />
      </CardContent>
      <div className="h-12 w-full animate-pulse bg-muted/40" />
    </Card>
  );
}

function DepStatus({ label, ok }: { label: string; ok: boolean }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-sm text-muted-foreground">{label}</span>
      {ok ? (
        <span className="flex items-center gap-1 text-xs font-medium text-success">
          <CheckCircle2Icon className="size-3.5" /> Up
        </span>
      ) : (
        <span className="flex items-center gap-1 text-xs font-medium text-destructive">
          <XCircleIcon className="size-3.5" /> Down
        </span>
      )}
    </div>
  );
}

const QUICK_ACTIONS = [
  { icon: ListFilterIcon, label: "Search Logs", desc: "Run LogQL++ queries", href: "/search", cls: "bg-primary/10 text-primary" },
  { icon: RadioIcon, label: "Live Tail", desc: "Stream logs in real time", href: "/tail", cls: "bg-info/10 text-info" },
  { icon: AlertTriangleIcon, label: "Alert Rules", desc: "Set up threshold alerts", href: "/alerts", cls: "bg-warning/10 text-warning" },
] as const;

// ── page ──────────────────────────────────────────────────────────────────────

function OverviewPage() {
  const healthQ = useHealth();
  const serviceQ = useServiceVolume();
  const hourlyQ = useHourlyVolume();
  const errorQ = useErrorCount();

  const health = healthQ.data;
  const allHealthy = health?.healthy ?? false;

  const totalEvents = serviceQ.data?.reduce((s, r) => s + Number(r.count ?? 0), 0) ?? 0;
  const errorCount = Number(errorQ.data?.[0]?.count ?? 0);
  const serviceCount = serviceQ.data?.length ?? 0;

  const hourlyRows: HourlyVolume[] = (hourlyQ.data ?? []).map((r) => ({
    hour: String(r.hour ?? "").slice(11, 16),
    count: Number(r.count ?? 0),
    errors: 0,
  }));

  const serviceRows: ServiceVolume[] = (serviceQ.data ?? []).map((r) => ({
    service: String(r.service ?? "unknown"),
    count: Number(r.count ?? 0),
  }));

  const sparkVolume = hourlyRows.slice(-12).map((r) => r.count);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {/* Title + health indicator */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground">Platform health and log ingestion metrics</p>
        </div>
        <div className="flex items-center gap-2">
          {healthQ.isLoading ? (
            <div className="h-4 w-24 animate-pulse rounded bg-muted" />
          ) : (
            <span className={`flex items-center gap-1.5 text-xs font-medium ${allHealthy ? "text-success" : "text-destructive"}`}>
              <PresenceIndicator status={allHealthy ? "online" : "away"} size="sm" pulse={allHealthy} />
              {allHealthy ? "All systems operational" : "Degraded — check health"}
            </span>
          )}
        </div>
      </div>

      {/* KPI cards */}
      <div className="grid auto-rows-min gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {serviceQ.isLoading ? (
          <>
            <StatSkeleton /><StatSkeleton /><StatSkeleton /><StatSkeleton />
          </>
        ) : (
          <>
            <StatCard
              icon={<ActivityIcon />}
              title="Total Log Events"
              value={totalEvents.toLocaleString("en-IN")}
              sparkData={sparkVolume}
              iconClass="bg-primary/10 text-primary"
            />
            <StatCard
              icon={<AlertTriangleIcon />}
              title="Error Events"
              value={errorCount.toLocaleString("en-IN")}
              iconClass="bg-destructive/10 text-destructive"
            />
            <StatCard
              icon={<ServerIcon />}
              title="Active Services"
              value={String(serviceCount)}
              iconClass="bg-info/10 text-info"
            />
            <StatCard
              icon={<DatabaseIcon />}
              title="Data Volume"
              value={totalEvents > 0 ? `~${(totalEvents * 0.5).toFixed(0)} KB` : "0 KB"}
              iconClass="bg-success/10 text-success"
            />
          </>
        )}
      </div>

      {/* Bento: volume trend + service breakdown */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-6">
        {/* 24h volume chart */}
        <Card className={`lg:col-span-4 ${cardLift}`}>
          <CardHeader>
            <CardTitle>Log Volume — Last 24 Hours</CardTitle>
            <CardDescription>Hourly event count from ClickHouse</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" render={<Link to="/search" />}>
                Search logs
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {hourlyQ.isLoading ? (
              <div className="h-64 w-full animate-pulse rounded bg-muted/40" />
            ) : hourlyRows.length === 0 ? (
              <EmptyState
                icon={ActivityIcon}
                title="No log events yet"
                description="Ingest logs via the API or SDK to see volume trends."
              />
            ) : (
              <ChartContainer config={volumeConfig} className="aspect-auto h-64 w-full">
                <AreaChart data={hourlyRows} margin={{ left: 0, right: 8 }}>
                  <defs>
                    <linearGradient id="fill-count" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="var(--color-count)" stopOpacity={0.5} />
                      <stop offset="100%" stopColor="var(--color-count)" stopOpacity={0.05} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
                  <XAxis dataKey="hour" tickLine={false} axisLine={false} tickMargin={8} />
                  <YAxis tickLine={false} axisLine={false} width={40} />
                  <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
                  <Area
                    type="monotone"
                    dataKey="count"
                    stroke="var(--color-count)"
                    fill="url(#fill-count)"
                    strokeWidth={2}
                  />
                </AreaChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>

        {/* Service breakdown */}
        <Card className={`lg:col-span-2 ${cardLift}`}>
          <CardHeader>
            <CardTitle>Top Services</CardTitle>
            <CardDescription>By log event count</CardDescription>
          </CardHeader>
          <CardContent>
            {serviceQ.isLoading ? (
              <div className="h-64 w-full animate-pulse rounded bg-muted/40" />
            ) : serviceRows.length === 0 ? (
              <EmptyState icon={ServerIcon} title="No services" description="No log events ingested." />
            ) : (
              <ChartContainer config={serviceConfig} className="aspect-auto h-64 w-full">
                <BarChart data={serviceRows} layout="vertical" margin={{ left: 0, right: 16 }}>
                  <CartesianGrid horizontal={false} stroke="var(--border)" strokeDasharray="3 3" />
                  <XAxis type="number" tickLine={false} axisLine={false} />
                  <YAxis type="category" dataKey="service" tickLine={false} axisLine={false} width={90} />
                  <ChartTooltip cursor={false} content={<ChartTooltipContent hideLabel />} />
                  <Bar dataKey="count" radius={[0, 6, 6, 0]}>
                    {serviceRows.map((_, i) => (
                      <Cell key={i} fill={`var(--chart-${(i % 5) + 1})`} />
                    ))}
                  </Bar>
                </BarChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Quick actions */}
      <div className="flex flex-col gap-2">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Quick Actions
        </h2>
        <div className="grid gap-3 sm:grid-cols-3">
          {QUICK_ACTIONS.map(({ icon: Icon, label, desc, href, cls }) => (
            <Link key={href} to={href as never} className="block">
              <Card className={`flex cursor-pointer items-center gap-3 p-4 transition-colors hover:bg-muted/40 ${cardLift}`}>
                <div className={`grid size-9 shrink-0 place-items-center rounded-lg [&_svg]:size-4 ${cls}`}>
                  <Icon />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium leading-none">{label}</p>
                  <p className="mt-1 truncate text-xs text-muted-foreground">{desc}</p>
                </div>
                <ChevronRightIcon className="ml-auto size-4 shrink-0 text-muted-foreground/40" />
              </Card>
            </Link>
          ))}
        </div>
      </div>

      {/* Dependency health */}
      <Card>
        <CardHeader>
          <CardTitle>System Health</CardTitle>
          <CardDescription>Infrastructure dependency status</CardDescription>
        </CardHeader>
        <CardContent>
          {healthQ.isLoading ? (
            <div className="space-y-3">
              {["PostgreSQL", "ClickHouse", "NATS", "Redis"].map((l) => (
                <div key={l} className="flex items-center justify-between">
                  <div className="h-3 w-24 animate-pulse rounded bg-muted" />
                  <div className="h-3 w-10 animate-pulse rounded bg-muted" />
                </div>
              ))}
            </div>
          ) : !health ? (
            <p className="text-sm text-muted-foreground">Unable to reach API.</p>
          ) : (
            <div className="space-y-3">
              <DepStatus label="PostgreSQL" ok={health.details.postgres} />
              <DepStatus label="ClickHouse" ok={health.details.clickhouse} />
              <DepStatus label="NATS JetStream" ok={health.details.nats} />
              <DepStatus label="Redis" ok={health.details.redis} />
            </div>
          )}
        </CardContent>
        <div className="border-t px-6 py-3">
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">Last checked {new Date().toLocaleTimeString()}</span>
            {allHealthy
              ? <Badge variant="secondary" className="border-success/40 bg-success/10 text-success text-xs">Healthy</Badge>
              : <Badge variant="destructive" className="text-xs">Degraded</Badge>}
          </div>
        </div>
      </Card>
    </div>
  );
}
