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
    meta: { successMessage: "Saved search deleted" },
  });

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title="Saved Searches"
        description="Reusable LogQL++ statements the team can re-run from Log Search in one click."
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
                title="No saved searches"
                description="Save a query from Log Search to pin it here for the whole tenant."
                action={
                  <Button render={<Link to="/search" />}>
                    <PlayIcon /> Go to Log Search
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Query</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-28 text-right">Actions</TableHead>
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
                          aria-label={`Run ${s.name}`}
                          render={<Link to="/search" search={{ q: s.query_text }} />}
                        >
                          <PlayIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Delete ${s.name}`}
                          onClick={() =>
                            confirm({
                              title: `Delete "${s.name}"?`,
                              confirmLabel: "Delete",
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
