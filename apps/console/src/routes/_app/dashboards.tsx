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
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
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
    meta: { successMessage: t("pages.dashboards.createdToast") },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/dashboards/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dashboards"] }),
    meta: { successMessage: t("pages.dashboards.deletedToast") },
  });

  function submit() {
    let panels: unknown = [];
    try {
      panels = panelsText.trim() ? JSON.parse(panelsText) : [];
      setPanelsError(null);
    } catch {
      setPanelsError(t("pages.dashboards.panelsInvalid"));
      return;
    }
    create.mutate({ name: name.trim(), panels });
  }

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title={t("pages.dashboards.title")}
        description={t("pages.dashboards.description")}
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> {t("pages.dashboards.newDashboard")}
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
                title={t("pages.dashboards.emptyTitle")}
                description={t("pages.dashboards.emptyDescription")}
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> {t("pages.dashboards.newDashboard")}
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
                    {t("pages.dashboards.updated", {
                      time: relativeTime(d.updated_at ?? d.created_at),
                    })}
                  </CardDescription>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("pages.dashboards.deleteAria", { name: d.name })}
                  onClick={() =>
                    confirm({
                      title: t("pages.dashboards.deleteTitle", { name: d.name }),
                      confirmLabel: t("actions.delete"),
                      onConfirm: () => remove.mutate(d.id),
                    })
                  }
                >
                  <Trash2Icon className="text-destructive" />
                </Button>
              </CardHeader>
              <CardContent>
                <Badge variant="muted">
                  {t("pages.dashboards.panels", { count: panelCount(d.panels) })}
                </Badge>
              </CardContent>
            </Card>
          ))}
        </div>
      </DataState>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.dashboards.createTitle")}</DialogTitle>
            <DialogDescription>{t("pages.dashboards.createDescription")}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="db-name">{t("fields.name")}</Label>
              <Input
                id="db-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("pages.dashboards.namePlaceholder")}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="db-panels">{t("pages.dashboards.panelsLabel")}</Label>
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
            <DialogClose render={<Button variant="outline">{t("actions.cancel")}</Button>} />
            <Button onClick={submit} disabled={!name.trim() || create.isPending}>
              {create.isPending && <Loader2Icon className="animate-spin" />}
              {t("actions.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
