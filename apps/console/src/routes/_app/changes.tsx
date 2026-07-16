import {
  Badge,
  Card,
  CardContent,
  DataState,
  EmptyState,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@qeetrix/ui";
import { createFileRoute } from "@tanstack/react-router";
import { GitBranchIcon } from "lucide-react";

import { PageHeader } from "@/components/page-header";
import { formatDateTime, relativeTime } from "@/lib/format";
import { useChanges } from "@/lib/incidents";

export const Route = createFileRoute("/_app/changes")({ component: ChangesPage });

function kindVariant(kind: string | undefined): "success" | "warning" | "secondary" | "muted" {
  switch ((kind ?? "").toLowerCase()) {
    case "deploy":
      return "success";
    case "config":
    case "flag":
      return "warning";
    case "infra":
      return "secondary";
    default:
      return "muted";
  }
}

function ChangesPage() {
  const q = useChanges();
  const changes = q.data ?? [];

  return (
    <>
      <PageHeader
        title="Changes"
        description="Every deploy, config edit and feature-flag flip — the change stream correlated against your incidents."
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={q.isLoading}
            isError={false}
            isEmpty={changes.length === 0}
            empty={
              <EmptyState
                icon={GitBranchIcon}
                title="No changes recorded"
                description="Wire a deploy webhook or CI hook to stream change markers into Qeet Logs."
              />
            }
            skeletonRows={6}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead>Change</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead className="text-right">When</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {changes.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell>
                      <Badge variant={kindVariant(c.kind)}>{c.kind ?? "change"}</Badge>
                    </TableCell>
                    <TableCell className="max-w-md truncate font-medium">
                      {c.summary ?? c.version ?? c.id}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{c.service ?? "—"}</TableCell>
                    <TableCell className="text-muted-foreground">{c.actor ?? "—"}</TableCell>
                    <TableCell
                      className="text-right text-muted-foreground"
                      title={formatDateTime(c.created_at ?? c.at)}
                    >
                      {relativeTime(c.created_at ?? c.at)}
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
