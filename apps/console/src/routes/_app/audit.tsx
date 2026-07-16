import {
  Badge,
  Card,
  CardContent,
  DataState,
  Input,
  StatusPill,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@qeetrix/ui";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ScrollTextIcon } from "lucide-react";
import { useState } from "react";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatDateTime, formatNumber } from "@/lib/format";

export const Route = createFileRoute("/_app/audit")({ component: AuditPage });

type AuditEntry = {
  id: number;
  actor?: string | null;
  action: string;
  resource?: string | null;
  resource_id?: string | null;
  status: string;
  ip?: string | null;
  created_at: string;
};
type AuditResponse = { entries: AuditEntry[]; total: number };

function statusKind(status: string): "success" | "danger" | "muted" {
  const s = status.toLowerCase();
  if (["ok", "success", "allowed"].includes(s)) return "success";
  if (["error", "denied", "failed"].includes(s)) return "danger";
  return "muted";
}

function AuditPage() {
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("");

  const q = useQuery({
    queryKey: ["audit", actor, action],
    queryFn: () =>
      api<AuditResponse>("/v1/admin/audit", {
        query: { actor: actor || undefined, action: action || undefined },
      }),
    placeholderData: keepPreviousData,
    retry: false,
    meta: { silent: true },
  });

  const entries = q.data?.entries ?? [];

  return (
    <>
      <PageHeader
        title="Audit Log"
        description="Tamper-evident record of every query and admin action for this tenant."
      />

      <Card>
        <CardContent className="flex flex-col gap-3 p-3 sm:flex-row sm:items-center">
          <Input
            placeholder="Filter by actor…"
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            className="sm:max-w-xs"
            aria-label="Filter by actor"
          />
          <Input
            placeholder="Filter by action…"
            value={action}
            onChange={(e) => setAction(e.target.value)}
            className="sm:max-w-xs"
            aria-label="Filter by action"
          />
          <span className="text-xs text-muted-foreground sm:ms-auto">
            {formatNumber(q.data?.total ?? 0)} total
          </span>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={q.isLoading}
            isError={q.isError}
            error={q.error}
            isEmpty={!q.isLoading && entries.length === 0}
            emptyIcon={ScrollTextIcon}
            emptyTitle="No audit entries"
            emptyDescription="Actions and queries appear here as they happen."
            skeletonRows={8}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Resource</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>IP</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDateTime(e.created_at)}
                    </TableCell>
                    <TableCell className="font-mono-logs text-xs">{e.actor ?? "—"}</TableCell>
                    <TableCell>
                      <Badge variant="muted">{e.action}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {e.resource ?? "—"}
                      {e.resource_id ? (
                        <span className="font-mono-logs text-xs"> · {e.resource_id}</span>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      <StatusPill kind={statusKind(e.status)}>{e.status}</StatusPill>
                    </TableCell>
                    <TableCell className="font-mono-logs text-xs text-muted-foreground">
                      {e.ip ?? "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </DataState>
        </CardContent>
      </Card>
    </>
  );
}
