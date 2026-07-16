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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
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
import { Building2Icon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatNumber } from "@/lib/format";

export const Route = createFileRoute("/_app/business-context")({ component: BusinessContextPage });

type BusinessContext = {
  id: string;
  service?: string;
  tier?: string;
  owner_team?: string;
  description?: string;
  revenue_per_hour?: number;
};

type NewContext = Omit<BusinessContext, "id">;

const TIERS = ["critical", "high", "standard", "low"] as const;

function asList(data: unknown): BusinessContext[] {
  if (Array.isArray(data)) return data as BusinessContext[];
  const obj = data as Record<string, unknown> | null;
  if (obj && Array.isArray(obj.items)) return obj.items as BusinessContext[];
  return [];
}

function BusinessContextPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<NewContext>({
    service: "",
    tier: "standard",
    owner_team: "",
    description: "",
    revenue_per_hour: 0,
  });

  const listQ = useQuery({
    queryKey: ["business-context"],
    queryFn: () => api<unknown>("/v1/admin/business-context").then(asList),
    retry: false,
    meta: { silent: true },
  });

  const create = useMutation({
    mutationFn: (body: NewContext) => api("/v1/admin/business-context", { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["business-context"] });
      setOpen(false);
      setForm({
        service: "",
        tier: "standard",
        owner_team: "",
        description: "",
        revenue_per_hour: 0,
      });
    },
    meta: { successMessage: t("pages.businessContext.savedToast") },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/business-context/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["business-context"] }),
    meta: { successMessage: t("pages.businessContext.deletedToast") },
  });

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title={t("pages.businessContext.title")}
        description={t("pages.businessContext.description")}
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> {t("pages.businessContext.addMapping")}
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
                icon={Building2Icon}
                title={t("pages.businessContext.emptyTitle")}
                description={t("pages.businessContext.emptyDescription")}
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> {t("pages.businessContext.addMapping")}
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.service")}</TableHead>
                  <TableHead>{t("columns.tier")}</TableHead>
                  <TableHead>{t("columns.owner")}</TableHead>
                  <TableHead>{t("columns.revenuePerHour")}</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="font-medium">{c.service ?? "—"}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{c.tier ?? "standard"}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{c.owner_team ?? "—"}</TableCell>
                    <TableCell className="text-muted-foreground">
                      ${formatNumber(c.revenue_per_hour ?? 0)}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={t("pages.businessContext.deleteAria", { service: c.service })}
                        onClick={() =>
                          confirm({
                            title: t("pages.businessContext.deleteTitle", { service: c.service }),
                            confirmLabel: t("actions.delete"),
                            onConfirm: () => remove.mutate(c.id),
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
            <DialogTitle>{t("pages.businessContext.dialogTitle")}</DialogTitle>
            <DialogDescription>{t("pages.businessContext.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="bc-service">{t("fields.service")}</Label>
                <Input
                  id="bc-service"
                  value={form.service}
                  onChange={(e) => setForm((f) => ({ ...f, service: e.target.value }))}
                  placeholder={t("pages.businessContext.servicePlaceholder")}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("fields.tier")}</Label>
                <Select
                  value={form.tier}
                  onValueChange={(v) => setForm((f) => ({ ...f, tier: v ?? "standard" }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TIERS.map((t) => (
                      <SelectItem key={t} value={t}>
                        {t}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="bc-owner">{t("pages.businessContext.owningTeam")}</Label>
                <Input
                  id="bc-owner"
                  value={form.owner_team}
                  onChange={(e) => setForm((f) => ({ ...f, owner_team: e.target.value }))}
                  placeholder={t("pages.businessContext.owningTeamPlaceholder")}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="bc-rev">{t("pages.businessContext.revenuePerHourDollar")}</Label>
                <Input
                  id="bc-rev"
                  type="number"
                  value={form.revenue_per_hour ?? 0}
                  onChange={(e) =>
                    setForm((f) => ({ ...f, revenue_per_hour: Number(e.target.value) || 0 }))
                  }
                />
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="bc-desc">{t("fields.description")}</Label>
              <Textarea
                id="bc-desc"
                rows={2}
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                placeholder={t("pages.businessContext.descriptionPlaceholder")}
              />
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">{t("actions.cancel")}</Button>} />
            <Button
              onClick={() => create.mutate(form)}
              disabled={!form.service?.trim() || create.isPending}
            >
              {create.isPending && <Loader2Icon className="animate-spin" />}
              {t("actions.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
