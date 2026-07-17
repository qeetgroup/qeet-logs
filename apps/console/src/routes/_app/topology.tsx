import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  DataState,
  EmptyState,
  StatusPill,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ArrowRightIcon, NetworkIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/topology")({ component: TopologyPage });

type TopoNode = {
  id: string;
  name?: string;
  service?: string;
  health?: string;
  error_rate?: number;
  rps?: number;
};
type TopoEdge = { source: string; target: string; calls?: number; error_rate?: number };
type Topology = { nodes: TopoNode[]; edges: TopoEdge[] };

function healthKind(node: TopoNode): "success" | "warning" | "danger" | "muted" {
  if (node.health) {
    const h = node.health.toLowerCase();
    if (h === "healthy" || h === "ok") return "success";
    if (h === "degraded" || h === "warn") return "warning";
    if (h === "down" || h === "unhealthy") return "danger";
  }
  const er = node.error_rate ?? 0;
  if (er >= 0.1) return "danger";
  if (er >= 0.02) return "warning";
  return "success";
}

function TopologyPage() {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: ["topology"],
    queryFn: () => api<Topology>("/v1/topology"),
    retry: false,
    meta: { silent: true },
  });

  const nodes = q.data?.nodes ?? [];
  const edges = q.data?.edges ?? [];
  const label = (id: string) => nodes.find((n) => n.id === id)?.name ?? id;

  return (
    <>
      <PageHeader title={t("pages.topology.title")} description={t("pages.topology.description")} />

      <DataState
        isLoading={q.isLoading}
        isError={q.isError}
        error={q.error}
        isEmpty={!q.isLoading && nodes.length === 0}
        empty={
          <Card>
            <CardContent className="pt-6">
              <EmptyState
                icon={NetworkIcon}
                title={t("pages.topology.emptyTitle")}
                description={t("pages.topology.emptyDescription")}
              />
            </CardContent>
          </Card>
        }
        skeletonRows={4}
      >
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {nodes.map((n) => {
            const deps = edges.filter((e) => e.source === n.id);
            return (
              <Card key={n.id}>
                <CardHeader className="flex-row items-center justify-between gap-2">
                  <CardTitle className="truncate text-sm">{n.name ?? n.service ?? n.id}</CardTitle>
                  <StatusPill kind={healthKind(n)} dot>
                    {n.health ??
                      t("pages.topology.errShort", {
                        percent: Math.round((n.error_rate ?? 0) * 100),
                      })}
                  </StatusPill>
                </CardHeader>
                <CardContent className="flex flex-col gap-2">
                  <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                    {typeof n.rps === "number" && (
                      <Badge variant="muted">{t("pages.topology.rps", { count: n.rps })}</Badge>
                    )}
                    {typeof n.error_rate === "number" && (
                      <Badge variant="muted">
                        {t("pages.topology.errorsPercent", {
                          percent: (n.error_rate * 100).toFixed(2),
                        })}
                      </Badge>
                    )}
                  </div>
                  {deps.length > 0 ? (
                    <ul className="flex flex-col gap-1">
                      {deps.map((e) => (
                        <li
                          key={`${e.source}->${e.target}`}
                          className="flex items-center gap-1.5 text-xs"
                        >
                          <ArrowRightIcon className="size-3.5 text-muted-foreground" />
                          <span className="truncate">{label(e.target)}</span>
                          {typeof e.error_rate === "number" && e.error_rate > 0 && (
                            <Badge variant="destructive" className="ms-auto">
                              {(e.error_rate * 100).toFixed(1)}%
                            </Badge>
                          )}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      {t("pages.topology.noDownstream")}
                    </p>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      </DataState>
    </>
  );
}
