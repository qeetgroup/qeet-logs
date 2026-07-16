import {
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
  DataState,
  EmptyState,
  Stat,
  StatusPill,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
  ActivityIcon,
  ChevronRightIcon,
  DatabaseIcon,
  FlameIcon,
  GaugeIcon,
  GitBranchIcon,
  HardDriveIcon,
} from "lucide-react";
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatBytes, formatNumber, relativeTime, severityKind } from "@/lib/format";
import { useChanges, useIncidents } from "@/lib/incidents";

export const Route = createFileRoute("/_app/")({ component: OverviewPage });

type Quota = {
  events?: number;
  bytes_stored?: number;
  retention_days?: number;
  period_start?: string;
  period_end?: string;
};

type AuditEntry = { action: string; created_at: string; status?: string };
type AuditResponse = { entries: AuditEntry[]; total: number };

const activityConfig = {
  events: { label: "Queries", color: "var(--chart-1)" },
} satisfies ChartConfig;

function bucketByHour(entries: AuditEntry[]): Array<{ hour: string; events: number }> {
  const buckets = new Map<string, number>();
  const now = new Date();
  for (let i = 23; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3_600_000);
    const key = `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:00`;
    buckets.set(key, 0);
  }
  for (const e of entries) {
    const d = new Date(e.created_at);
    if (Number.isNaN(d.getTime())) continue;
    const key = `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:00`;
    if (buckets.has(key)) buckets.set(key, (buckets.get(key) ?? 0) + 1);
  }
  return [...buckets.entries()].map(([hour, events]) => ({ hour, events }));
}

function OverviewPage() {
  const quotaQ = useQuery({
    queryKey: ["quota"],
    queryFn: () => api<Quota>("/v1/admin/quota/usage"),
    retry: false,
    meta: { silent: true },
  });
  const auditQ = useQuery({
    queryKey: ["audit", "overview"],
    queryFn: () => api<AuditResponse>("/v1/admin/audit"),
    retry: false,
    meta: { silent: true },
  });
  const incidentsQ = useIncidents();
  const changesQ = useChanges();

  const series = bucketByHour(auditQ.data?.entries ?? []);
  const openIncidents = (incidentsQ.data ?? []).slice(0, 6);
  const recentChanges = (changesQ.data ?? []).slice(0, 6);

  return (
    <>
      <PageHeader
        title="Overview"
        description="Ingest health, query activity, open incidents and the changes that may have caused them."
      />

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          label="Events this period"
          value={formatNumber(quotaQ.data?.events ?? 0)}
          icon={ActivityIcon}
          hint="Current billing month"
        />
        <Stat
          label="Stored"
          value={formatBytes(quotaQ.data?.bytes_stored ?? 0)}
          icon={HardDriveIcon}
          hint="Compressed log body"
        />
        <Stat
          label="Retention"
          value={`${quotaQ.data?.retention_days ?? "—"} days`}
          icon={DatabaseIcon}
          hint="Tenant policy"
        />
        <Stat
          label="Open incidents"
          value={formatNumber(incidentsQ.data?.length ?? 0)}
          icon={FlameIcon}
          hint="Awaiting triage"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <GaugeIcon className="size-4 text-muted-foreground" />
            Query activity — last 24h
          </CardTitle>
          <CardDescription>
            Queries recorded in the tamper-evident audit log, by hour.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <DataState
            isLoading={auditQ.isLoading}
            isError={auditQ.isError}
            error={auditQ.error}
            isEmpty={!auditQ.isLoading && (auditQ.data?.entries.length ?? 0) === 0}
            emptyIcon={GaugeIcon}
            emptyTitle="No recorded activity"
            emptyDescription="Run a query from Log Search to populate the audit trail."
            skeletonRows={4}
          >
            <ChartContainer config={activityConfig} className="aspect-auto h-56 w-full">
              <AreaChart data={series} margin={{ left: 4, right: 4, top: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey="hour"
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  minTickGap={32}
                  tickFormatter={(v: string) => v.split(" ")[1] ?? v}
                />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Area
                  dataKey="events"
                  type="monotone"
                  fill="var(--color-events)"
                  fillOpacity={0.2}
                  stroke="var(--color-events)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ChartContainer>
          </DataState>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FlameIcon className="size-4 text-muted-foreground" />
              Open incidents
            </CardTitle>
            <CardDescription>Correlated error clusters awaiting triage.</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <DataState
              isLoading={incidentsQ.isLoading}
              isError={false}
              isEmpty={openIncidents.length === 0}
              empty={
                <EmptyState
                  icon={FlameIcon}
                  title="No open incidents"
                  description="Incident intelligence correlates errors into incidents as they arrive."
                />
              }
              skeletonRows={4}
            >
              <ul className="divide-y">
                {openIncidents.map((inc) => (
                  <li key={inc.id}>
                    <Link
                      to="/incidents"
                      className="flex items-center gap-3 px-4 py-3 hover:bg-muted/50"
                    >
                      <StatusPill kind={severityKind(inc.severity)}>
                        {String(inc.severity ?? "info")}
                      </StatusPill>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium">
                          {inc.title ?? inc.summary ?? inc.id}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                          {inc.service ?? "unknown service"} ·{" "}
                          {relativeTime(inc.opened_at ?? inc.last_seen)}
                        </div>
                      </div>
                      <ChevronRightIcon className="size-4 text-muted-foreground" />
                    </Link>
                  </li>
                ))}
              </ul>
            </DataState>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranchIcon className="size-4 text-muted-foreground" />
              Recent changes
            </CardTitle>
            <CardDescription>Deploys and config changes overlaid on your logs.</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <DataState
              isLoading={changesQ.isLoading}
              isError={false}
              isEmpty={recentChanges.length === 0}
              empty={
                <EmptyState
                  icon={GitBranchIcon}
                  title="No recent changes"
                  description="Connect a deploy source to see change markers on incidents and charts."
                />
              }
              skeletonRows={4}
            >
              <ul className="divide-y">
                {recentChanges.map((c) => (
                  <li key={c.id} className="flex items-center gap-3 px-4 py-3">
                    <span className="rounded bg-muted px-2 py-0.5 text-xs font-medium uppercase text-muted-foreground">
                      {c.kind ?? "deploy"}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">
                        {c.summary ?? c.version ?? c.id}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {c.service ?? "—"} · {relativeTime(c.created_at ?? c.at)}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            </DataState>
            <div className="border-t p-3">
              <Button variant="outline" size="sm" render={<Link to="/changes" />}>
                View all changes
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </>
  );
}
