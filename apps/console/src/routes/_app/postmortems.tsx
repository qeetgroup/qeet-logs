import {
  Button,
  Card,
  CardContent,
  DataState,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  Input,
  Label,
  StatusPill,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { FileDownIcon, FileTextIcon, Loader2Icon, PlusIcon } from "lucide-react";
import { useState } from "react";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { downloadText, formatDateTime } from "@/lib/format";

export const Route = createFileRoute("/_app/postmortems")({ component: PostmortemsPage });

type Postmortem = {
  id: string;
  title?: string;
  incident_id?: string;
  status?: string;
  summary?: string;
  author?: string;
  created_at?: string;
};

function asList(data: unknown): Postmortem[] {
  if (Array.isArray(data)) return data as Postmortem[];
  const obj = data as Record<string, unknown> | null;
  if (obj && Array.isArray(obj.items)) return obj.items as Postmortem[];
  return [];
}

function PostmortemsPage() {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ title: "", incident_id: "", summary: "" });

  const listQ = useQuery({
    queryKey: ["postmortems"],
    queryFn: () => api<unknown>("/v1/admin/postmortems").then(asList),
    retry: false,
    meta: { silent: true },
  });

  const create = useMutation({
    mutationFn: (body: { title: string; incident_id?: string; summary?: string }) =>
      api("/v1/admin/postmortems", { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["postmortems"] });
      setOpen(false);
      setForm({ title: "", incident_id: "", summary: "" });
    },
    meta: { successMessage: "Postmortem created" },
  });

  const certExport = useMutation({
    mutationFn: (id: string) => api<unknown>(`/v1/admin/postmortems/${id}/cert-in-export`),
    onSuccess: (report, id) =>
      downloadText(
        `postmortem-${id}-cert-in.json`,
        JSON.stringify(report, null, 2),
        "application/json",
      ),
    meta: { successMessage: "CERT-In report downloaded" },
  });

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title="Postmortems"
        description="Incident writeups with a one-click CERT-In (Indian 6-hour incident reporting) export."
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> New postmortem
          </Button>
        }
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={listQ.isLoading}
            isError={false}
            isEmpty={!listQ.isLoading && rows.length === 0}
            empty={
              <EmptyState
                icon={FileTextIcon}
                title="No postmortems"
                description="Write up a resolved incident to capture the timeline, root cause and follow-ups."
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> New postmortem
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Author</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-32 text-right">CERT-In</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="max-w-md truncate font-medium">
                      {p.title ?? p.id}
                    </TableCell>
                    <TableCell>
                      <StatusPill kind={p.status === "published" ? "success" : "muted"}>
                        {p.status ?? "draft"}
                      </StatusPill>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{p.author ?? "—"}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatDateTime(p.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={certExport.isPending}
                        onClick={() => certExport.mutate(p.id)}
                      >
                        <FileDownIcon /> Export
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </DataState>
        </CardContent>
      </Card>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New postmortem</DialogTitle>
            <DialogDescription>Capture the writeup for a resolved incident.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="pm-title">Title</Label>
              <Input
                id="pm-title"
                value={form.title}
                onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
                placeholder="Checkout 5xx spike — 2026-07-14"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="pm-incident">Incident ID (optional)</Label>
              <Input
                id="pm-incident"
                className="font-mono-logs"
                value={form.incident_id}
                onChange={(e) => setForm((f) => ({ ...f, incident_id: e.target.value }))}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="pm-summary">Summary</Label>
              <Textarea
                id="pm-summary"
                rows={4}
                value={form.summary}
                onChange={(e) => setForm((f) => ({ ...f, summary: e.target.value }))}
                placeholder="What happened, impact, root cause, and follow-up actions."
              />
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">Cancel</Button>} />
            <Button
              onClick={() =>
                create.mutate({
                  title: form.title.trim(),
                  incident_id: form.incident_id.trim() || undefined,
                  summary: form.summary.trim() || undefined,
                })
              }
              disabled={!form.title.trim() || create.isPending}
            >
              {create.isPending && <Loader2Icon className="animate-spin" />}
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
