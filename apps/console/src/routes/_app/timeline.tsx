import {
  Badge,
  Card,
  CardContent,
  DataState,
  EmptyState,
  Timeline,
  TimelineContent,
  TimelineDescription,
  TimelineIndicator,
  TimelineItem,
  TimelineTime,
  TimelineTitle,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { HistoryIcon } from "lucide-react";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatDateTime, relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/timeline")({ component: TimelinePage });

type TimelineEvent = {
  id: string;
  at?: string;
  timestamp?: string;
  kind?: string; // incident | deploy | alert | config
  title?: string;
  description?: string;
  service?: string;
  severity?: string;
};

function kindVariant(
  kind: string | undefined,
): "default" | "destructive" | "warning" | "success" | "muted" {
  switch ((kind ?? "").toLowerCase()) {
    case "incident":
    case "alert":
      return "destructive";
    case "deploy":
      return "success";
    case "config":
    case "flag":
      return "warning";
    default:
      return "muted";
  }
}

function TimelinePage() {
  const q = useQuery({
    queryKey: ["timeline"],
    queryFn: () => api<{ events?: TimelineEvent[] } | TimelineEvent[]>("/v1/timeline"),
    retry: false,
    meta: { silent: true },
  });

  const events: TimelineEvent[] = Array.isArray(q.data) ? q.data : (q.data?.events ?? []);

  return (
    <>
      <PageHeader
        title="Timeline"
        description="A unified chronology of incidents, deploys, alerts and config changes across your services."
      />

      <Card>
        <CardContent className="pt-6">
          <DataState
            isLoading={q.isLoading}
            isError={q.isError}
            error={q.error}
            isEmpty={!q.isLoading && events.length === 0}
            empty={
              <EmptyState
                icon={HistoryIcon}
                title="Nothing on the timeline yet"
                description="Incidents, deploys and alerts appear here in order as they happen."
              />
            }
            skeletonRows={6}
          >
            <Timeline>
              {events.map((e) => (
                <TimelineItem key={e.id}>
                  <TimelineIndicator />
                  <TimelineContent>
                    <TimelineTitle className="flex items-center gap-2">
                      <Badge variant={kindVariant(e.kind)}>{e.kind ?? "event"}</Badge>
                      <span className="truncate">{e.title ?? e.id}</span>
                      {e.service ? (
                        <span className="text-xs text-muted-foreground">· {e.service}</span>
                      ) : null}
                    </TimelineTitle>
                    <TimelineTime title={formatDateTime(e.at ?? e.timestamp)}>
                      {relativeTime(e.at ?? e.timestamp)}
                    </TimelineTime>
                    {e.description && <TimelineDescription>{e.description}</TimelineDescription>}
                  </TimelineContent>
                </TimelineItem>
              ))}
            </Timeline>
          </DataState>
        </CardContent>
      </Card>
    </>
  );
}
