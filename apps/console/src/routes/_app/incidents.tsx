import {
  Badge,
  Button,
  Card,
  CardContent,
  DataState,
  EmptyState,
  Progress,
  Separator,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  StatusPill,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@qeetrix/ui";
import { createFileRoute } from "@tanstack/react-router";
import {
  BrainCircuitIcon,
  Building2Icon,
  FlameIcon,
  GitCommitHorizontalIcon,
  ThumbsDownIcon,
  ThumbsUpIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/page-header";
import type { Incident } from "@/lib/domain";
import { formatNumber, relativeTime, severityKind } from "@/lib/format";
import {
  useDeployCulprits,
  useIncidentContext,
  useIncidentFeedback,
  useIncidents,
  useRca,
} from "@/lib/incidents";

export const Route = createFileRoute("/_app/incidents")({ component: IncidentsPage });

function IncidentsPage() {
  const { t } = useTranslation();
  const incidentsQ = useIncidents();
  const [selected, setSelected] = useState<Incident | null>(null);

  return (
    <>
      <PageHeader
        title={t("pages.incidents.title")}
        description={t("pages.incidents.description")}
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={incidentsQ.isLoading}
            isError={false}
            isEmpty={(incidentsQ.data?.length ?? 0) === 0}
            empty={
              <EmptyState
                icon={FlameIcon}
                title={t("pages.incidents.emptyTitle")}
                description={t("pages.incidents.emptyDescription")}
              />
            }
            skeletonRows={6}
          >
            <ul className="divide-y">
              {(incidentsQ.data ?? []).map((inc) => (
                <li key={inc.id}>
                  <button
                    type="button"
                    onClick={() => setSelected(inc)}
                    className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/50"
                  >
                    <StatusPill kind={severityKind(inc.severity)}>
                      {String(inc.severity ?? "info")}
                    </StatusPill>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">
                        {inc.title ?? inc.summary ?? inc.id}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {inc.service ?? t("pages.incidents.unknownService")} ·{" "}
                        {t("pages.incidents.eventsCount", { count: inc.count ?? 0 })} ·{" "}
                        {relativeTime(inc.opened_at ?? inc.last_seen)}
                      </div>
                    </div>
                    {inc.status ? <Badge variant="outline">{inc.status}</Badge> : null}
                  </button>
                </li>
              ))}
            </ul>
          </DataState>
        </CardContent>
      </Card>

      <IncidentSheet incident={selected} onClose={() => setSelected(null)} />
    </>
  );
}

function IncidentSheet({ incident, onClose }: { incident: Incident | null; onClose: () => void }) {
  const { t } = useTranslation();
  const feedback = useIncidentFeedback();

  return (
    <Sheet open={!!incident} onOpenChange={(o) => !o && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-xl">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <StatusPill kind={severityKind(incident?.severity)}>
              {String(incident?.severity ?? "info")}
            </StatusPill>
            <span className="truncate">{incident?.title ?? incident?.summary ?? incident?.id}</span>
          </SheetTitle>
          <SheetDescription>
            {incident?.service ?? t("pages.incidents.unknownService")} ·{" "}
            {t("pages.incidents.eventsCount", { count: incident?.count ?? 0 })} ·{" "}
            {t("pages.incidents.openedRelative", {
              time: relativeTime(incident?.opened_at ?? incident?.first_seen),
            })}
          </SheetDescription>
        </SheetHeader>

        {incident && (
          <div className="flex flex-col gap-4 overflow-y-auto px-4 pb-4">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">
                {t("pages.incidents.actionablePrompt")}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={feedback.isPending}
                onClick={() => feedback.mutate({ id: incident.id, verdict: "actionable" })}
              >
                <ThumbsUpIcon /> {t("pages.incidents.actionable")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={feedback.isPending}
                onClick={() => feedback.mutate({ id: incident.id, verdict: "noise" })}
              >
                <ThumbsDownIcon /> {t("pages.incidents.noise")}
              </Button>
            </div>

            <Separator />

            <Tabs defaultValue="rca">
              <TabsList>
                <TabsTrigger value="rca">
                  <BrainCircuitIcon className="size-4" /> {t("pages.incidents.tabRca")}
                </TabsTrigger>
                <TabsTrigger value="deploys">
                  <GitCommitHorizontalIcon className="size-4" /> {t("pages.incidents.tabDeploys")}
                </TabsTrigger>
                <TabsTrigger value="business">
                  <Building2Icon className="size-4" /> {t("pages.incidents.tabBusiness")}
                </TabsTrigger>
              </TabsList>
              <TabsContent value="rca" className="pt-4">
                <RcaPanel service={incident.service} />
              </TabsContent>
              <TabsContent value="deploys" className="pt-4">
                <DeployPanel service={incident.service} />
              </TabsContent>
              <TabsContent value="business" className="pt-4">
                <BusinessPanel id={incident.id} />
              </TabsContent>
            </Tabs>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function RcaPanel({ service }: { service?: string }) {
  const { t } = useTranslation();
  const q = useRca(service);
  return (
    <DataState
      isLoading={q.isLoading}
      isError={q.isError}
      error={q.error}
      isEmpty={!q.isLoading && !q.data}
      emptyIcon={BrainCircuitIcon}
      emptyTitle={t("pages.incidents.rcaEmptyTitle")}
      emptyDescription={t("pages.incidents.rcaEmptyDescription")}
      skeletonRows={4}
    >
      <div className="flex flex-col gap-4">
        {q.data?.summary && <p className="text-sm">{q.data.summary}</p>}
        {typeof q.data?.confidence === "number" && (
          <div className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">
              {t("pages.incidents.confidence", {
                percent: Math.round((q.data.confidence ?? 0) * 100),
              })}
            </span>
            <Progress value={Math.round((q.data.confidence ?? 0) * 100)} />
          </div>
        )}
        {(q.data?.hypotheses ?? []).map((h, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: hypotheses have no id
          <Card key={i}>
            <CardContent className="flex flex-col gap-1 pt-4">
              <div className="text-sm font-medium">
                {h.title ?? t("pages.incidents.hypothesis", { index: i + 1 })}
              </div>
              {h.detail && <p className="text-xs text-muted-foreground">{h.detail}</p>}
            </CardContent>
          </Card>
        ))}
      </div>
    </DataState>
  );
}

function DeployPanel({ service }: { service?: string }) {
  const { t } = useTranslation();
  const q = useDeployCulprits(service);
  return (
    <DataState
      isLoading={q.isLoading}
      isError={q.isError}
      error={q.error}
      isEmpty={!q.isLoading && (q.data?.length ?? 0) === 0}
      emptyIcon={GitCommitHorizontalIcon}
      emptyTitle={t("pages.incidents.deploysEmptyTitle")}
      emptyDescription={t("pages.incidents.deploysEmptyDescription")}
      skeletonRows={4}
    >
      <ul className="flex flex-col gap-2">
        {(q.data ?? []).map((c, i) => (
          <li
            key={c.deploy_id ?? i}
            className="flex items-center gap-3 rounded-md border px-3 py-2"
          >
            <Badge variant={i === 0 ? "destructive" : "outline"}>
              {Math.round((c.score ?? 0) * 100)}%
            </Badge>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{c.version ?? c.deploy_id}</div>
              <div className="truncate text-xs text-muted-foreground">
                {c.reason ?? t("pages.incidents.correlatedReason")} · {relativeTime(c.deployed_at)}
              </div>
            </div>
          </li>
        ))}
      </ul>
    </DataState>
  );
}

function BusinessPanel({ id }: { id: string }) {
  const { t } = useTranslation();
  const q = useIncidentContext(id);
  const d = q.data;
  return (
    <DataState
      isLoading={q.isLoading}
      isError={q.isError}
      error={q.error}
      isEmpty={!q.isLoading && !d}
      emptyIcon={Building2Icon}
      emptyTitle={t("pages.incidents.businessEmptyTitle")}
      emptyDescription={t("pages.incidents.businessEmptyDescription")}
      skeletonRows={4}
    >
      <div className="grid grid-cols-2 gap-3">
        <Metric
          label={t("pages.incidents.affectedCustomers")}
          value={formatNumber(d?.affected_customers ?? 0)}
        />
        <Metric
          label={t("pages.incidents.revenueAtRisk")}
          value={`${d?.currency ?? "$"}${formatNumber(d?.affected_revenue ?? 0)}`}
        />
        <Metric label={t("fields.tier")} value={d?.tier ?? "—"} />
        <Metric
          label={t("pages.incidents.slaBreach")}
          value={d?.sla_breach ? t("pages.incidents.yes") : t("pages.incidents.no")}
        />
      </div>
      {d?.summary && <p className="mt-3 text-sm text-muted-foreground">{d.summary}</p>}
    </DataState>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold">{value}</div>
    </div>
  );
}
