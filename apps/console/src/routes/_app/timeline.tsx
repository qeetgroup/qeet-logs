import {
  Badge,
  Button,
  Card,
  CardContent,
  EmptyState,
  Input,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { GitCommitVerticalIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/timeline")({ component: TimelinePage });

type TimelineEvent = {
  type: "log" | "span" | "deploy";
  timestamp: string;
  service: string;
  severity: string;
  title: string;
  trace_id?: string;
};

type TimelineResponse = { count: number; events: TimelineEvent[] };

const TYPE_VARIANT: Record<string, "secondary" | "outline" | "destructive"> = {
  log: "outline",
  span: "secondary",
  deploy: "destructive",
};

function isError(sev: string) {
  return sev === "error" || sev === "fatal";
}

function TimelinePage() {
  const [traceInput, setTraceInput] = useState("");
  const [trace, setTrace] = useState("");

  const q = useQuery({
    queryKey: ["timeline", trace],
    queryFn: () =>
      api<TimelineResponse>("/v1/timeline", {
        query: trace ? { trace_id: trace } : { since: 3600 },
      }),
  });

  const events = q.data?.events ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Investigation Timeline</h1>
          <p className="text-sm text-muted-foreground">
            One chronological feed across logs, spans, and deploys
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => q.refetch()} disabled={q.isFetching}>
          <RefreshCwIcon className={`mr-1.5 size-4 ${q.isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      <Card>
        <CardContent className="py-4">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setTrace(traceInput.trim());
            }}
          >
            <Input
              placeholder="Focus a trace_id (blank = last hour, warn+)"
              value={traceInput}
              onChange={(e) => setTraceInput(e.target.value)}
            />
            <Button type="submit">Focus</Button>
          </form>
        </CardContent>
      </Card>

      {q.isLoading ? (
        <div className="flex items-center justify-center py-16 text-muted-foreground">
          <Loader2Icon className="mr-2 size-5 animate-spin" /> Building timeline…
        </div>
      ) : events.length === 0 ? (
        <EmptyState icon={<GitCommitVerticalIcon />} title="No events" description="Nothing in this window." />
      ) : (
        <ol className="relative flex flex-col gap-2 border-l pl-4">
          {events.map((e, i) => (
            <li key={i} className="relative">
              <span
                className={`absolute -left-[21px] top-1.5 size-2.5 rounded-full ${
                  isError(e.severity) ? "bg-destructive" : "bg-muted-foreground"
                }`}
              />
              <div className="flex items-center gap-2 text-sm">
                <Badge variant={TYPE_VARIANT[e.type]}>{e.type}</Badge>
                <span className="font-mono text-xs text-muted-foreground">{e.timestamp}</span>
                <span className="font-medium">{e.service}</span>
                {e.severity && (
                  <span className={isError(e.severity) ? "text-destructive" : "text-muted-foreground"}>
                    {e.severity}
                  </span>
                )}
              </div>
              <div className="truncate text-sm text-foreground/90">{e.title}</div>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
