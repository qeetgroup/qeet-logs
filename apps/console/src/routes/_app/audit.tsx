import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ClipboardListIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/audit")({ component: AuditPage });

type AuditEntry = {
  id: string;
  tenant_id: string;
  actor: string;
  action: string;
  resource: string;
  resource_id: string | null;
  status: "ok" | "denied" | "error";
  ip: string | null;
  user_agent: string | null;
  created_at: string;
};

type AuditResponse = {
  entries: AuditEntry[];
  total: number;
};

const STATUS_VARIANT: Record<string, "secondary" | "destructive" | "outline"> = {
  ok: "secondary",
  denied: "destructive",
  error: "outline",
};

function useAuditLog(actor: string, action: string) {
  const params: Record<string, string> = {};
  if (actor) params.actor = actor;
  if (action) params.action = action;

  return useQuery({
    queryKey: ["audit-log", actor, action],
    queryFn: () => api<AuditResponse>("/v1/admin/audit", { query: params }),
    meta: { silent: true },
  });
}

function AuditPage() {
  const [actorFilter, setActorFilter] = useState("");
  const [actionFilter, setActionFilter] = useState("");
  const [committedActor, setCommittedActor] = useState("");
  const [committedAction, setCommittedAction] = useState("");

  const auditQ = useAuditLog(committedActor, committedAction);

  function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    setCommittedActor(actorFilter.trim());
    setCommittedAction(actionFilter.trim());
  }

  const entries = auditQ.data?.entries ?? [];
  const total = auditQ.data?.total ?? 0;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Audit Log</h1>
          <p className="text-sm text-muted-foreground">
            Immutable record of all admin and query actions
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => auditQ.refetch()}
          disabled={auditQ.isFetching}
        >
          <RefreshCwIcon className={`mr-1.5 size-4 ${auditQ.isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {/* Filters */}
      <Card>
        <CardContent className="py-4">
          <form onSubmit={handleSearch} className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="flex-1">
              <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="audit-actor">
                Actor (sub / key prefix)
              </label>
              <Input
                id="audit-actor"
                placeholder="user:123, qeel_abc"
                value={actorFilter}
                onChange={(e) => setActorFilter(e.target.value)}
              />
            </div>
            <div className="flex-1">
              <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="audit-action">
                Action
              </label>
              <Input
                id="audit-action"
                placeholder="api-key.create, query.exec"
                value={actionFilter}
                onChange={(e) => setActionFilter(e.target.value)}
              />
            </div>
            <Button type="submit" className="shrink-0">
              Filter
            </Button>
          </form>
        </CardContent>
      </Card>

      {/* Results */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Events</CardTitle>
            <CardDescription>
              {auditQ.isLoading ? (
                <Loader2Icon className="size-4 animate-spin text-muted-foreground" />
              ) : (
                `${total.toLocaleString("en-IN")} total`
              )}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          {auditQ.isLoading ? (
            <div className="px-6 py-12 text-center">
              <Loader2Icon className="mx-auto size-6 animate-spin text-muted-foreground" />
            </div>
          ) : entries.length === 0 ? (
            <div className="px-6 py-12">
              <EmptyState
                icon={ClipboardListIcon}
                title="No audit events"
                description={
                  committedActor || committedAction
                    ? "No events matched the current filter."
                    : "Audit events will appear here as actions are performed."
                }
              >
                {(committedActor || committedAction) && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      setActorFilter("");
                      setActionFilter("");
                      setCommittedActor("");
                      setCommittedAction("");
                    }}
                  >
                    Clear filter
                  </Button>
                )}
              </EmptyState>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Actor</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Resource</TableHead>
                    <TableHead>Resource ID</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>IP</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.map((entry) => (
                    <TableRow key={entry.id}>
                      <TableCell className="whitespace-nowrap text-muted-foreground">
                        <TimeSince value={entry.created_at} />
                      </TableCell>
                      <TableCell className="max-w-[160px] truncate font-mono text-xs">
                        {entry.actor}
                      </TableCell>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {entry.action}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {entry.resource}
                      </TableCell>
                      <TableCell className="max-w-[120px] truncate font-mono text-xs text-muted-foreground">
                        {entry.resource_id ?? "—"}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={STATUS_VARIANT[entry.status] ?? "outline"}
                          className="uppercase text-[10px]"
                        >
                          {entry.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {entry.ip ?? "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
