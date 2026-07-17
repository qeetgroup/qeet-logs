import {
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
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ChartColumnIcon, GaugeIcon, TimerIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { CartesianGrid, Line, LineChart, XAxis } from "recharts";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatNumber } from "@/lib/format";

export const Route = createFileRoute("/_app/analytics")({ component: AnalyticsPage });

type TtfiqPoint = { bucket?: string; date?: string; p50_ms?: number; p90_ms?: number };
type Ttfiq = {
  p50_ms?: number;
  p90_ms?: number;
  p99_ms?: number;
  mean_ms?: number;
  samples?: number;
  series?: TtfiqPoint[];
};

const chartConfig = {
  p50_ms: { label: "p50 (ms)", color: "var(--chart-1)" },
  p90_ms: { label: "p90 (ms)", color: "var(--chart-2)" },
} satisfies ChartConfig;

function ms(v: number | undefined): string {
  if (v === undefined || Number.isNaN(v)) return "—";
  return v >= 1000 ? `${(v / 1000).toFixed(2)}s` : `${Math.round(v)}ms`;
}

function AnalyticsPage() {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["analytics", "ttfiq"],
    queryFn: () => api<Ttfiq>("/v1/analytics/ttfiq"),
    retry: false,
    meta: { silent: true },
  });

  const series = (q.data?.series ?? []).map((p) => ({
    label: p.bucket ?? p.date ?? "",
    p50_ms: p.p50_ms ?? 0,
    p90_ms: p.p90_ms ?? 0,
  }));

  return (
    <>
      <PageHeader
        title={t("pages.analytics.title")}
        description={t("pages.analytics.description")}
      />

      <DataState
        isLoading={q.isLoading}
        isError={q.isError}
        error={q.error}
        isEmpty={!q.isLoading && !q.data}
        empty={
          <Card>
            <CardContent className="pt-6">
              <EmptyState
                icon={ChartColumnIcon}
                title={t("pages.analytics.emptyTitle")}
                description={t("pages.analytics.emptyDescription")}
              />
            </CardContent>
          </Card>
        }
        skeletonRows={4}
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Stat label={t("pages.analytics.p50")} value={ms(q.data?.p50_ms)} icon={TimerIcon} />
          <Stat label={t("pages.analytics.p90")} value={ms(q.data?.p90_ms)} icon={TimerIcon} />
          <Stat label={t("pages.analytics.p99")} value={ms(q.data?.p99_ms)} icon={GaugeIcon} />
          <Stat
            label={t("pages.analytics.samples")}
            value={formatNumber(q.data?.samples ?? 0)}
            icon={ChartColumnIcon}
          />
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{t("pages.analytics.seriesTitle")}</CardTitle>
            <CardDescription>{t("pages.analytics.seriesDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            {series.length === 0 ? (
              <p className="py-10 text-center text-sm text-muted-foreground">
                {t("pages.analytics.noSeries")}
              </p>
            ) : (
              <ChartContainer config={chartConfig} className="aspect-auto h-64 w-full">
                <LineChart data={series} margin={{ left: 4, right: 4, top: 8 }}>
                  <CartesianGrid vertical={false} />
                  <XAxis
                    dataKey="label"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                    minTickGap={32}
                  />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Line dataKey="p50_ms" stroke="var(--color-p50_ms)" strokeWidth={2} dot={false} />
                  <Line dataKey="p90_ms" stroke="var(--color-p90_ms)" strokeWidth={2} dot={false} />
                </LineChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>
      </DataState>
    </>
  );
}
