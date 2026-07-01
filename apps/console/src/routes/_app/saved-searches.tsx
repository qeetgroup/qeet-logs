import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  Input,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { BookmarkIcon, ExternalLinkIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/saved-searches")({ component: SavedSearchesPage });

type SavedSearch = {
  id: string;
  tenant_id: string;
  name: string;
  query_text: string;
  created_by: string | null;
  created_at: string;
};

function useSavedSearches() {
  return useQuery({
    queryKey: ["saved-searches"],
    queryFn: () => api<SavedSearch[]>("/v1/admin/saved-searches"),
  });
}

// ── Create Sheet ─────────────────────────────────────────────────────────────

function CreateSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [errors, setErrors] = useState<Record<string, string>>({});

  const createM = useMutation({
    mutationFn: (data: { name: string; query_text: string }) =>
      api<SavedSearch>("/v1/admin/saved-searches", { method: "POST", body: data }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["saved-searches"] });
      onOpenChange(false);
      setErrors({});
    },
    meta: { successMessage: "Search saved" },
  });

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const name = String(fd.get("name") ?? "").trim();
    const query_text = String(fd.get("query_text") ?? "").trim();
    const errs: Record<string, string> = {};
    if (!name) errs.name = "Name is required.";
    if (!query_text) errs.query_text = "Query is required.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    createM.mutate({ name, query_text });
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>Save Search</SheetTitle>
          <SheetDescription>
            Bookmark a LogQL++ query so you can run it again instantly.
          </SheetDescription>
        </SheetHeader>
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <div className="flex-1 overflow-y-auto px-4 py-3">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="ss-name">Name</FieldLabel>
                <Input id="ss-name" name="name" placeholder="Production errors last 24h" autoFocus />
                {errors.name && <FieldError>{errors.name}</FieldError>}
              </Field>
              <Field>
                <FieldLabel htmlFor="ss-query">LogQL++ query</FieldLabel>
                <textarea
                  id="ss-query"
                  name="query_text"
                  rows={5}
                  defaultValue="SELECT * FROM logs WHERE level = 'error' LIMIT 50"
                  className="w-full resize-none rounded-md border bg-muted/30 p-3 font-mono text-sm focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary"
                  spellCheck={false}
                />
                {errors.query_text && <FieldError>{errors.query_text}</FieldError>}
              </Field>
            </FieldGroup>
          </div>
          <SheetFooter className="border-t px-4 py-3">
            <SheetClose render={<Button variant="outline" type="button" />}>Cancel</SheetClose>
            <Button type="submit" disabled={createM.isPending}>
              {createM.isPending && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
              Save Search
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────

function SavedSearchesPage() {
  const navigate = useNavigate();
  const searchesQ = useSavedSearches();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);

  const deleteM = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/saved-searches/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-searches"] }),
    meta: { successMessage: "Saved search deleted" },
  });

  function openInSearch(query: string) {
    navigate({ to: "/search", search: { q: query } as never });
  }

  const searches = searchesQ.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Saved Searches</h1>
          <p className="text-sm text-muted-foreground">Bookmark queries to run them instantly</p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <PlusIcon className="mr-1.5 size-4" />Save Search
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Bookmarks</CardTitle>
          <CardDescription>
            {searches.length} saved search{searches.length !== 1 ? "es" : ""}
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {searchesQ.isLoading ? (
            <div className="px-6 py-12 text-center">
              <Loader2Icon className="mx-auto size-6 animate-spin text-muted-foreground" />
            </div>
          ) : searches.length === 0 ? (
            <div className="px-6 py-12">
              <EmptyState
                icon={BookmarkIcon}
                title="No saved searches"
                description="Save a LogQL++ query to revisit it without retyping."
              >
                <Button onClick={() => setCreating(true)}>
                  <PlusIcon className="mr-1.5 size-4" />Save first search
                </Button>
              </EmptyState>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Query</TableHead>
                  <TableHead>Saved</TableHead>
                  <TableHead className="w-28 text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {searches.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-medium">{s.name}</TableCell>
                    <TableCell className="max-w-xs">
                      <Badge variant="outline" className="max-w-full truncate font-mono text-[10px]">
                        {s.query_text.length > 60
                          ? s.query_text.slice(0, 58) + "…"
                          : s.query_text}
                      </Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      <TimeSince value={s.created_at} />
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7 text-muted-foreground hover:text-primary"
                          onClick={() => openInSearch(s.query_text)}
                          title="Open in Log Search"
                        >
                          <ExternalLinkIcon className="size-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7 text-muted-foreground hover:text-destructive"
                          onClick={() => deleteM.mutate(s.id)}
                          disabled={deleteM.isPending}
                          title="Delete"
                        >
                          <Trash2Icon className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <CreateSheet open={creating} onOpenChange={setCreating} />
    </div>
  );
}
