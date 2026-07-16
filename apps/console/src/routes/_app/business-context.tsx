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
    meta: { successMessage: "Business context saved" },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/business-context/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["business-context"] }),
    meta: { successMessage: "Business context deleted" },
  });

  const rows = listQ.data ?? [];

  return (
    <>
      <PageHeader
        title="Business Context"
        description="Map services to their business owner, tier and revenue so incidents surface real-world impact."
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> Add mapping
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
                title="No business context"
                description="Add a mapping to enrich incidents with affected revenue, owning team and service tier."
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> Add mapping
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Service</TableHead>
                  <TableHead>Tier</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>Revenue / hr</TableHead>
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
                        aria-label={`Delete ${c.service}`}
                        onClick={() =>
                          confirm({
                            title: `Delete mapping for "${c.service}"?`,
                            confirmLabel: "Delete",
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
            <DialogTitle>Business context</DialogTitle>
            <DialogDescription>Describe what a service means to the business.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="bc-service">Service</Label>
                <Input
                  id="bc-service"
                  value={form.service}
                  onChange={(e) => setForm((f) => ({ ...f, service: e.target.value }))}
                  placeholder="checkout"
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label>Tier</Label>
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
                <Label htmlFor="bc-owner">Owning team</Label>
                <Input
                  id="bc-owner"
                  value={form.owner_team}
                  onChange={(e) => setForm((f) => ({ ...f, owner_team: e.target.value }))}
                  placeholder="payments"
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="bc-rev">Revenue / hr ($)</Label>
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
              <Label htmlFor="bc-desc">Description</Label>
              <Textarea
                id="bc-desc"
                rows={2}
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                placeholder="Handles order checkout and payment capture."
              />
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">Cancel</Button>} />
            <Button
              onClick={() => create.mutate(form)}
              disabled={!form.service?.trim() || create.isPending}
            >
              {create.isPending && <Loader2Icon className="animate-spin" />}
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
