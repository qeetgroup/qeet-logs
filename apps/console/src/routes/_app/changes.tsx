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
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
  const q = useChanges();
  const changes = q.data ?? [];

  return (
    <>
      <PageHeader title={t("pages.changes.title")} description={t("pages.changes.description")} />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={q.isLoading}
            isError={false}
            isEmpty={changes.length === 0}
            empty={
              <EmptyState
                icon={GitBranchIcon}
                title={t("pages.changes.emptyTitle")}
                description={t("pages.changes.emptyDescription")}
              />
            }
            skeletonRows={6}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.type")}</TableHead>
                  <TableHead>{t("columns.change")}</TableHead>
                  <TableHead>{t("columns.service")}</TableHead>
                  <TableHead>{t("columns.actor")}</TableHead>
                  <TableHead className="text-right">{t("columns.when")}</TableHead>
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
