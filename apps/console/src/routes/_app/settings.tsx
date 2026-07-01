import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Separator,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon, SaveIcon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/settings")({ component: SettingsPage });

type RetentionConfig = {
  retention_days: number;
  masking_actions: Record<string, string>;
};

type MaskingField = { key: string; action: string };

const MASKING_OPTIONS = ["mask", "hash", "drop_field", "drop_record"] as const;
const DEFAULT_FIELDS = ["email", "ip", "card", "phone", "jwt"];

function useRetention() {
  return useQuery({
    queryKey: ["retention-config"],
    queryFn: () => api<RetentionConfig>("/v1/admin/retention"),
    meta: { silent: true },
  });
}

function SettingsPage() {
  const qc = useQueryClient();
  const retentionQ = useRetention();
  const [retDays, setRetDays] = useState<number | null>(null);
  const [masking, setMasking] = useState<MaskingField[] | null>(null);

  const config = retentionQ.data;
  const effectiveDays = retDays ?? config?.retention_days ?? 7;

  const effectiveMasking: MaskingField[] = masking ?? (
    config
      ? DEFAULT_FIELDS.map((k) => ({
          key: k,
          action: config.masking_actions[k] ?? "mask",
        }))
      : DEFAULT_FIELDS.map((k) => ({ key: k, action: "mask" }))
  );

  const saveM = useMutation({
    mutationFn: (data: { retention_days: number; masking_actions: Record<string, string> }) =>
      api("/v1/admin/retention", { method: "PUT", body: data }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["retention-config"] }),
    meta: { successMessage: "Settings saved" },
  });

  function handleSave() {
    const actions = Object.fromEntries(effectiveMasking.map((m) => [m.key, m.action]));
    saveM.mutate({ retention_days: effectiveDays, masking_actions: actions });
  }

  function updateMasking(key: string, action: string) {
    setMasking((prev) => {
      const base = prev ?? effectiveMasking;
      return base.map((m) => (m.key === key ? { ...m, action } : m));
    });
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <div>
        <h1 className="font-heading text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">Retention policy and PII masking configuration</p>
      </div>

      {/* Retention */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Log Retention</CardTitle>
              <CardDescription>
                Per-record TTL — logs are hard-deleted from ClickHouse at the configured age.
              </CardDescription>
            </div>
            <Badge variant="outline" className="border-primary/40 text-primary text-xs">
              Per-record TTL
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          {retentionQ.isLoading ? (
            <div className="h-10 w-40 animate-pulse rounded bg-muted" />
          ) : (
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="ret-days">Retention window (days)</FieldLabel>
                <Input
                  id="ret-days"
                  type="number"
                  min={1}
                  max={3650}
                  value={effectiveDays}
                  onChange={(e) => setRetDays(Number(e.target.value))}
                  className="w-40"
                />
                <FieldDescription>
                  Applied as <code className="text-xs">_retention_days</code> on each ingested record.
                  ClickHouse TTL enforces deletion automatically.
                </FieldDescription>
              </Field>
            </FieldGroup>
          )}
        </CardContent>
      </Card>

      {/* PII Masking */}
      <Card>
        <CardHeader>
          <CardTitle>PII Masking</CardTitle>
          <CardDescription>
            Actions applied synchronously at ingest time, before logs reach ClickHouse.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {effectiveMasking.map((m) => (
              <div key={m.key} className="flex items-center gap-4">
                <span className="w-20 font-mono text-sm capitalize">{m.key}</span>
                <Select value={m.action} onValueChange={(v) => v != null && updateMasking(m.key, v)}>
                  <SelectTrigger className="w-44">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MASKING_OPTIONS.map((opt) => (
                      <SelectItem key={opt} value={opt}>
                        {opt === "mask" && "Mask (*** redacted ***)"}
                        {opt === "hash" && "Hash (SHA-256)"}
                        {opt === "drop_field" && "Drop field"}
                        {opt === "drop_record" && "Drop record"}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <span className="text-xs text-muted-foreground">
                  {m.action === "mask" && "Replaces value with redacted placeholder"}
                  {m.action === "hash" && "SHA-256 hex digest; reversible with known input"}
                  {m.action === "drop_field" && "Removes the field from the record"}
                  {m.action === "drop_record" && "Discards the entire log event"}
                </span>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Separator />

      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={saveM.isPending}>
          {saveM.isPending ? (
            <Loader2Icon className="mr-1.5 size-4 animate-spin" />
          ) : (
            <SaveIcon className="mr-1.5 size-4" />
          )}
          Save Settings
        </Button>
      </div>
    </div>
  );
}
