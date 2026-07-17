import {
  Button,
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
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { BookmarkIcon, PlayIcon, Trash2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/saved-searches")({ component: SavedSearchesPage });

type SavedSearch = {
  id: string;
  name: string;
  query_text: string;
  created_by?: string | null;
  created_at?: string;
};

function SavedSearchesPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();

  const listQ = useQuery({
    queryKey: ["saved-searches"],
    queryFn: () => api<SavedSearch[]>("/v1/admin/saved-searches"),
    retry: false,
    meta: { silent: true },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/saved-searches/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-searches"] }),
    meta: { successMessage: t("pages.savedSearches.deletedToast") },
  });

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title={t("pages.savedSearches.title")}
        description={t("pages.savedSearches.description")}
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={listQ.isLoading}
            isError={listQ.isError}
            error={listQ.error}
            isEmpty={!listQ.isLoading && rows.length === 0}
            empty={
              <EmptyState
                icon={BookmarkIcon}
                title={t("pages.savedSearches.emptyTitle")}
                description={t("pages.savedSearches.emptyDescription")}
                action={
                  <Button render={<Link to="/search" />}>
                    <PlayIcon /> {t("pages.savedSearches.goToSearch")}
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.name")}</TableHead>
                  <TableHead>{t("columns.query")}</TableHead>
                  <TableHead>{t("columns.created")}</TableHead>
                  <TableHead className="w-28 text-right">{t("columns.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-medium">{s.name}</TableCell>
                    <TableCell className="max-w-md truncate font-mono-logs text-xs text-muted-foreground">
                      {s.query_text}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {relativeTime(s.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("pages.savedSearches.runAria", { name: s.name })}
                          render={<Link to="/search" search={{ q: s.query_text }} />}
                        >
                          <PlayIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("pages.savedSearches.deleteAria", { name: s.name })}
                          onClick={() =>
                            confirm({
                              title: t("pages.savedSearches.deleteTitle", { name: s.name }),
                              confirmLabel: t("actions.delete"),
                              onConfirm: () => remove.mutate(s.id),
                            })
                          }
                        >
                          <Trash2Icon className="text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </DataState>
        </CardContent>
      </Card>

      {dialog}
    </>
  );
}
