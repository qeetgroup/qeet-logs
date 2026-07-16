import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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
  Textarea,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { LayoutPanelLeftIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/dashboards")({ component: DashboardsPage });

type Dashboard = {
  id: string;
  name: string;
  panels?: unknown;
  created_by?: string | null;
  created_at?: string;
  updated_at?: string;
};

function panelCount(panels: unknown): number {
  if (Array.isArray(panels)) return panels.length;
  return 0;
}

function DashboardsPage() {
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [panelsText, setPanelsText] = useState("[]");
  const [panelsError, setPanelsError] = useState<string | null>(null);

  const listQ = useQuery({
    queryKey: ["dashboards"],
    queryFn: () => api<Dashboard[]>("/v1/admin/dashboards"),
    retry: false,
    meta: { silent: true },
  });

  const create = useMutation({
    mutationFn: (body: { name: string; panels: unknown }) =>
      api("/v1/admin/dashboards", { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["dashboards"] });
      setOpen(false);
      setName("");
      setPanelsText("[]");
    },
    meta: { successMessage: "Dashboard created" },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/dashboards/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dashboards"] }),
    meta: { successMessage: "Dashboard deleted" },
  });

  function submit() {
    let panels: unknown = [];
    try {
      panels = panelsText.trim() ? JSON.parse(panelsText) : [];
      setPanelsError(null);
    } catch {
      setPanelsError("Panels must be valid JSON.");
      return;
    }
    create.mutate({ name: name.trim(), panels });
  }

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title="Dashboards"
        description="Saved panel layouts of LogQL++ charts and tables for the tenant."
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> New dashboard
          </Button>
        }
      />

      <DataState
        isLoading={listQ.isLoading}
        isError={listQ.isError}
        error={listQ.error}
        isEmpty={!listQ.isLoading && rows.length === 0}
        empty={
          <Card>
            <CardContent className="pt-6">
              <EmptyState
                icon={LayoutPanelLeftIcon}
                title="No dashboards"
                description="Compose charts and tables into a saved dashboard for at-a-glance monitoring."
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> New dashboard
                  </Button>
                }
              />
            </CardContent>
          </Card>
        }
        skeletonRows={4}
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {rows.map((d) => (
            <Card key={d.id}>
              <CardHeader className="flex-row items-start justify-between gap-2">
                <div className="min-w-0">
                  <CardTitle className="truncate">{d.name}</CardTitle>
                  <CardDescription>
                    Updated {relativeTime(d.updated_at ?? d.created_at)}
                  </CardDescription>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Delete ${d.name}`}
                  onClick={() =>
                    confirm({
                      title: `Delete "${d.name}"?`,
                      confirmLabel: "Delete",
                      onConfirm: () => remove.mutate(d.id),
                    })
                  }
                >
                  <Trash2Icon className="text-destructive" />
                </Button>
              </CardHeader>
              <CardContent>
                <Badge variant="muted">{panelCount(d.panels)} panels</Badge>
              </CardContent>
            </Card>
          ))}
        </div>
      </DataState>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New dashboard</DialogTitle>
            <DialogDescription>
              Name the dashboard and optionally seed its panel layout as JSON.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="db-name">Name</Label>
              <Input
                id="db-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Checkout health"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="db-panels">Panels (JSON)</Label>
              <Textarea
                id="db-panels"
                rows={5}
                className="font-mono-logs text-xs"
                value={panelsText}
                onChange={(e) => setPanelsText(e.target.value)}
              />
              {panelsError && <p className="text-xs text-destructive">{panelsError}</p>}
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">Cancel</Button>} />
            <Button onClick={submit} disabled={!name.trim() || create.isPending}>
              {create.isPending && <Loader2Icon className="animate-spin" />}
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
