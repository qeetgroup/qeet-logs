import {
  Badge,
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
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon, PlusIcon, Trash2Icon, WebhookIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/webhooks")({ component: WebhooksPage });

type Webhook = {
  id: string;
  url?: string;
  events?: string[];
  enabled?: boolean;
  created_at?: string;
};

const EVENTS = ["incident.opened", "incident.resolved", "alert.fired", "deploy.detected"] as const;

function asList(data: unknown): Webhook[] {
  if (Array.isArray(data)) return data as Webhook[];
  const obj = data as Record<string, unknown> | null;
  if (obj && Array.isArray(obj.items)) return obj.items as Webhook[];
  return [];
}

function WebhooksPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<Set<string>>(new Set(["incident.opened"]));

  const listQ = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api<unknown>("/v1/admin/webhooks").then(asList),
    retry: false,
    meta: { silent: true },
  });

  const create = useMutation({
    mutationFn: (body: { url: string; events: string[] }) =>
      api("/v1/admin/webhooks", { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["webhooks"] });
      setOpen(false);
      setUrl("");
      setEvents(new Set(["incident.opened"]));
    },
    meta: { successMessage: t("pages.webhooks.createdToast") },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/webhooks/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
    meta: { successMessage: t("pages.webhooks.deletedToast") },
  });

  function toggleEvent(e: string) {
    setEvents((prev) => {
      const next = new Set(prev);
      if (next.has(e)) next.delete(e);
      else next.add(e);
      return next;
    });
  }

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title={t("pages.webhooks.title")}
        description={t("pages.webhooks.description")}
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> {t("pages.webhooks.newWebhook")}
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
                icon={WebhookIcon}
                title={t("pages.webhooks.emptyTitle")}
                description={t("pages.webhooks.emptyDescription")}
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> {t("pages.webhooks.newWebhook")}
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.endpoint")}</TableHead>
                  <TableHead>{t("columns.events")}</TableHead>
                  <TableHead>{t("columns.status")}</TableHead>
                  <TableHead>{t("columns.created")}</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((w) => (
                  <TableRow key={w.id}>
                    <TableCell className="max-w-md truncate font-mono-logs text-xs">
                      {w.url ?? "—"}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(w.events ?? []).map((e) => (
                          <Badge key={e} variant="muted" className="text-[10px]">
                            {e}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusPill kind={w.enabled === false ? "muted" : "success"}>
                        {w.enabled === false
                          ? t("pages.webhooks.disabled")
                          : t("pages.webhooks.active")}
                      </StatusPill>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {relativeTime(w.created_at)}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={t("pages.webhooks.deleteAria")}
                        onClick={() =>
                          confirm({
                            title: t("pages.webhooks.deleteTitle"),
                            description: w.url,
                            confirmLabel: t("actions.delete"),
                            onConfirm: () => remove.mutate(w.id),
                          })
                        }
                      >
                        <Trash2Icon className="text-destructive" />
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
            <DialogTitle>{t("pages.webhooks.createTitle")}</DialogTitle>
            <DialogDescription>{t("pages.webhooks.createDescription")}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="wh-url">{t("pages.webhooks.endpointUrl")}</Label>
              <Input
                id="wh-url"
                className="font-mono-logs"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={t("pages.webhooks.urlPlaceholder")}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label>{t("fields.events")}</Label>
              <div className="flex flex-wrap gap-2">
                {EVENTS.map((e) => {
                  const on = events.has(e);
                  return (
                    <Button
                      key={e}
                      type="button"
                      size="sm"
                      variant={on ? "default" : "outline"}
                      className="text-xs"
                      onClick={() => toggleEvent(e)}
                    >
                      {e}
                    </Button>
                  );
                })}
              </div>
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">{t("actions.cancel")}</Button>} />
            <Button
              onClick={() => create.mutate({ url: url.trim(), events: [...events] })}
              disabled={!url.trim() || events.size === 0 || create.isPending}
            >
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
