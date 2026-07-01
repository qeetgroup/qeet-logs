import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { AlertTriangleIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/alerts")({ component: AlertsPage });

type AlertRule = {
  id: string;
  name: string;
  kind: "threshold" | "absence";
  service?: string;
  condition?: string;
  threshold?: number;
  window_seconds: number;
  channels: Array<{ type: string; target: string }>;
  enabled: boolean;
  created_at: string;
};

type CreateAlertInput = {
  name: string;
  kind: "threshold" | "absence";
  service?: string;
  condition?: string;
  threshold?: number;
  window_seconds: number;
  channels: Array<{ type: string; target: string }>;
};

function useAlerts() {
  return useQuery({
    queryKey: ["alerts"],
    queryFn: () => api<AlertRule[]>("/v1/admin/alert-rules"),
    meta: { silent: true },
  });
}

function CreateSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [kind, setKind] = useState<"threshold" | "absence">("threshold");
  const [errors, setErrors] = useState<Record<string, string>>({});

  const createM = useMutation({
    mutationFn: (data: CreateAlertInput) =>
      api<AlertRule>("/v1/admin/alert-rules", { method: "POST", body: data }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["alerts"] });
      onOpenChange(false);
    },
    meta: { successMessage: "Alert rule created" },
  });

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const name = String(fd.get("name") ?? "").trim();
    const service = String(fd.get("service") ?? "").trim();
    const condition = String(fd.get("condition") ?? "").trim();
    const windowSec = Number(fd.get("window_seconds") ?? 300);
    const threshold = kind === "threshold" ? Number(fd.get("threshold") ?? 0) : undefined;
    const webhookTarget = String(fd.get("webhook_target") ?? "").trim();

    const errs: Record<string, string> = {};
    if (!name) errs.name = "Name is required.";
    if (kind === "threshold" && (!threshold || threshold <= 0)) errs.threshold = "Must be > 0.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;

    createM.mutate({
      name,
      kind,
      service: service || undefined,
      condition: condition || undefined,
      threshold,
      window_seconds: windowSec,
      channels: webhookTarget ? [{ type: "webhook", target: webhookTarget }] : [],
    });
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>New Alert Rule</SheetTitle>
          <SheetDescription>
            Fires when the condition is met over the evaluation window.
          </SheetDescription>
        </SheetHeader>
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <div className="flex-1 overflow-y-auto px-4 py-3">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="a-name">Name</FieldLabel>
                <Input id="a-name" name="name" placeholder="High error rate" />
                {errors.name && <FieldError>{errors.name}</FieldError>}
              </Field>
              <Field>
                <FieldLabel htmlFor="a-kind">Type</FieldLabel>
                <Select name="kind" value={kind} onValueChange={(v) => setKind(v as typeof kind)}>
                  <SelectTrigger id="a-kind"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="threshold">Threshold — fires when count exceeds N</SelectItem>
                    <SelectItem value="absence">Absence — fires when no logs for N seconds</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="a-service">Service (optional)</FieldLabel>
                <Input id="a-service" name="service" placeholder="payments, auth, …" />
                <FieldDescription>Leave blank to evaluate across all services.</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="a-condition">Condition (optional)</FieldLabel>
                <Input id="a-condition" name="condition" placeholder="level = 'error'" />
                <FieldDescription>LogQL++ WHERE fragment applied before counting.</FieldDescription>
              </Field>
              {kind === "threshold" && (
                <Field>
                  <FieldLabel htmlFor="a-threshold">Threshold count</FieldLabel>
                  <Input id="a-threshold" name="threshold" type="number" min={1} defaultValue={10} />
                  {errors.threshold && <FieldError>{errors.threshold}</FieldError>}
                </Field>
              )}
              <Field>
                <FieldLabel htmlFor="a-window">Evaluation window (seconds)</FieldLabel>
                <Input id="a-window" name="window_seconds" type="number" min={60} defaultValue={300} />
              </Field>
              <Field>
                <FieldLabel htmlFor="a-webhook">Webhook URL (optional)</FieldLabel>
                <Input id="a-webhook" name="webhook_target" type="url" placeholder="https://…" />
                <FieldDescription>POST JSON payload on alert state change.</FieldDescription>
              </Field>
            </FieldGroup>
          </div>
          <SheetFooter className="border-t px-4 py-3">
            <SheetClose render={<Button variant="outline" type="button" />}>Cancel</SheetClose>
            <Button type="submit" disabled={createM.isPending}>
              {createM.isPending && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
              Create Rule
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

function AlertsPage() {
  const alertsQ = useAlerts();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);

  const deleteM = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/alert-rules/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["alerts"] });
      setDeleting(null);
    },
    meta: { successMessage: "Alert rule deleted" },
  });

  const rules = alertsQ.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Alert Rules</h1>
          <p className="text-sm text-muted-foreground">Threshold and absence alerting</p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <PlusIcon className="mr-1.5 size-4" /> New Rule
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Rules</CardTitle>
          <CardDescription>{rules.length} rule{rules.length !== 1 ? "s" : ""} configured</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {alertsQ.isLoading ? (
            <div className="px-6 py-12 text-center">
              <Loader2Icon className="mx-auto size-6 animate-spin text-muted-foreground" />
            </div>
          ) : rules.length === 0 ? (
            <div className="px-6 py-12">
              <EmptyState
                icon={AlertTriangleIcon}
                title="No alert rules"
                description="Create a threshold or absence rule to get notified when something goes wrong."
              >
                <Button onClick={() => setCreating(true)}>
                  <PlusIcon className="mr-1.5 size-4" /> Create first rule
                </Button>
              </EmptyState>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Window</TableHead>
                  <TableHead>Channels</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={rule.id}>
                    <TableCell className="font-medium">{rule.name}</TableCell>
                    <TableCell>
                      <Badge variant="outline" className="capitalize">{rule.kind}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{rule.service ?? "all"}</TableCell>
                    <TableCell className="text-muted-foreground">{rule.window_seconds}s</TableCell>
                    <TableCell className="text-muted-foreground">
                      {rule.channels.length > 0
                        ? rule.channels.map((c) => c.type).join(", ")
                        : "—"}
                    </TableCell>
                    <TableCell>
                      <Switch checked={rule.enabled} aria-label="Toggle rule" />
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      <TimeSince value={rule.created_at} />
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setDeleting(rule.id)}
                      >
                        <Trash2Icon className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <CreateSheet open={creating} onOpenChange={setCreating} />

      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this alert rule?</AlertDialogTitle>
            <AlertDialogDescription>
              This is permanent and cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              variant="destructive"
              onClick={() => deleting && deleteM.mutate(deleting)}
              disabled={deleteM.isPending}
            >
              {deleteM.isPending && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
              Delete
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
