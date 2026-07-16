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
import { BellRingIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/alerts")({ component: AlertsPage });

type AlertRule = {
  id: string;
  name: string;
  kind: "threshold" | "absence" | string;
  service?: string | null;
  condition?: string | null;
  threshold?: number | null;
  window_seconds: number;
  channels?: unknown;
  enabled: boolean;
  created_at: string;
};

type NewRule = {
  name: string;
  kind: "threshold" | "absence";
  service?: string;
  condition?: string;
  threshold?: number;
  window_seconds: number;
  channels: string[];
};

function AlertsPage() {
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<NewRule>({
    name: "",
    kind: "threshold",
    service: "",
    condition: "",
    threshold: 10,
    window_seconds: 300,
    channels: [],
  });
  const [channelsText, setChannelsText] = useState("");

  const rulesQ = useQuery({
    queryKey: ["alert-rules"],
    queryFn: () => api<AlertRule[]>("/v1/admin/alert-rules"),
    retry: false,
    meta: { silent: true },
  });

  const createRule = useMutation({
    mutationFn: (body: NewRule) => api("/v1/admin/alert-rules", { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["alert-rules"] });
      setOpen(false);
      setForm({
        name: "",
        kind: "threshold",
        service: "",
        condition: "",
        threshold: 10,
        window_seconds: 300,
        channels: [],
      });
      setChannelsText("");
    },
    meta: { successMessage: "Alert rule created" },
  });

  const deleteRule = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/alert-rules/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-rules"] }),
    meta: { successMessage: "Alert rule deleted" },
  });

  function submit() {
    const channels = channelsText
      .split(",")
      .map((c) => c.trim())
      .filter(Boolean);
    createRule.mutate({
      ...form,
      service: form.service?.trim() || undefined,
      condition: form.condition?.trim() || undefined,
      channels,
    });
  }

  const rules = rulesQ.data ?? [];

  return (
    <>
      <PageHeader
        title="Alert Rules"
        description="Threshold and absence rules evaluated by the alerter engine; matches notify the configured channels."
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> New rule
          </Button>
        }
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={rulesQ.isLoading}
            isError={rulesQ.isError}
            error={rulesQ.error}
            isEmpty={!rulesQ.isLoading && rules.length === 0}
            empty={
              <EmptyState
                icon={BellRingIcon}
                title="No alert rules"
                description="Create a threshold or absence rule to get notified when your logs cross a limit."
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> New rule
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
                  <TableHead>Kind</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Window</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className="font-medium">{r.name}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{r.kind}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{r.service ?? "all"}</TableCell>
                    <TableCell className="text-muted-foreground">{r.window_seconds}s</TableCell>
                    <TableCell>
                      <StatusPill kind={r.enabled ? "success" : "muted"}>
                        {r.enabled ? "Enabled" : "Disabled"}
                      </StatusPill>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {relativeTime(r.created_at)}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete ${r.name}`}
                        onClick={() =>
                          confirm({
                            title: `Delete "${r.name}"?`,
                            description: "This alert rule will stop evaluating immediately.",
                            confirmLabel: "Delete",
                            onConfirm: () => deleteRule.mutate(r.id),
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
            <DialogTitle>New alert rule</DialogTitle>
            <DialogDescription>
              Threshold rules fire when a count crosses a limit; absence rules fire when expected
              logs stop arriving.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="rule-name">Name</Label>
              <Input
                id="rule-name"
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="High 5xx rate on checkout"
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label>Kind</Label>
                <Select
                  value={form.kind}
                  onValueChange={(v) => setForm((f) => ({ ...f, kind: v as NewRule["kind"] }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="threshold">Threshold</SelectItem>
                    <SelectItem value="absence">Absence</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="rule-service">Service (optional)</Label>
                <Input
                  id="rule-service"
                  value={form.service}
                  onChange={(e) => setForm((f) => ({ ...f, service: e.target.value }))}
                  placeholder="checkout"
                />
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="rule-condition">Condition</Label>
              <Input
                id="rule-condition"
                className="font-mono-logs"
                value={form.condition}
                onChange={(e) => setForm((f) => ({ ...f, condition: e.target.value }))}
                placeholder='level = "error"'
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-2">
                <Label htmlFor="rule-threshold">Threshold</Label>
                <Input
                  id="rule-threshold"
                  type="number"
                  value={form.threshold ?? ""}
                  onChange={(e) =>
                    setForm((f) => ({ ...f, threshold: Number(e.target.value) || undefined }))
                  }
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="rule-window">Window (seconds)</Label>
                <Input
                  id="rule-window"
                  type="number"
                  value={form.window_seconds}
                  onChange={(e) =>
                    setForm((f) => ({ ...f, window_seconds: Number(e.target.value) || 300 }))
                  }
                />
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="rule-channels">Channels (comma-separated)</Label>
              <Input
                id="rule-channels"
                value={channelsText}
                onChange={(e) => setChannelsText(e.target.value)}
                placeholder="email:oncall@acme.com, slack:#alerts"
              />
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">Cancel</Button>} />
            <Button onClick={submit} disabled={!form.name.trim() || createRule.isPending}>
              {createRule.isPending && <Loader2Icon className="animate-spin" />}
              Create rule
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
