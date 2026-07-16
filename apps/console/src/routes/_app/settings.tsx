import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  DataState,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon, PlusIcon, SaveIcon, Trash2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatDateTime } from "@/lib/format";

export const Route = createFileRoute("/_app/settings")({ component: SettingsPage });

type Retention = {
  retention_days: number;
  masking_actions: Record<string, string>;
  updated_at?: string;
};

const ACTIONS = ["redact", "hash", "mask", "drop"] as const;
type MaskRow = { field: string; action: string };

function SettingsPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [days, setDays] = useState(7);
  const [rows, setRows] = useState<MaskRow[]>([]);

  const cfgQ = useQuery({
    queryKey: ["retention"],
    queryFn: () => api<Retention>("/v1/admin/retention"),
    retry: false,
    meta: { silent: true },
  });

  useEffect(() => {
    if (cfgQ.data) {
      setDays(cfgQ.data.retention_days);
      setRows(
        Object.entries(cfgQ.data.masking_actions ?? {}).map(([field, action]) => ({
          field,
          action,
        })),
      );
    }
  }, [cfgQ.data]);

  const save = useMutation({
    mutationFn: (body: Retention) => api("/v1/admin/retention", { method: "PUT", body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["retention"] }),
    meta: { successMessage: t("pages.settings.saved") },
  });

  function submit() {
    const masking_actions: Record<string, string> = {};
    for (const r of rows) {
      if (r.field.trim()) masking_actions[r.field.trim()] = r.action;
    }
    save.mutate({ retention_days: days, masking_actions });
  }

  return (
    <>
      <PageHeader
        title={t("pages.settings.title")}
        description={t("pages.settings.description")}
        actions={
          <Button onClick={submit} disabled={save.isPending}>
            {save.isPending ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
            {t("actions.saveChanges")}
          </Button>
        }
      />

      <DataState
        isLoading={cfgQ.isLoading}
        isError={cfgQ.isError}
        error={cfgQ.error}
        skeletonRows={4}
      >
        <div className="grid gap-4">
          <Card>
            <CardHeader>
              <CardTitle>{t("pages.settings.retentionTitle")}</CardTitle>
              <CardDescription>
                {t("pages.settings.retentionDescription")}
                {cfgQ.data?.updated_at
                  ? t("pages.settings.lastUpdated", {
                      date: formatDateTime(cfgQ.data.updated_at),
                    })
                  : ""}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex max-w-xs flex-col gap-2">
                <Label htmlFor="retention-days">{t("pages.settings.retentionDaysLabel")}</Label>
                <Input
                  id="retention-days"
                  type="number"
                  min={1}
                  max={3650}
                  value={days}
                  onChange={(e) => setDays(Number(e.target.value) || 1)}
                />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <div>
                <CardTitle>{t("pages.settings.transformsTitle")}</CardTitle>
                <CardDescription>{t("pages.settings.transformsDescription")}</CardDescription>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRows((r) => [...r, { field: "", action: "redact" }])}
              >
                <PlusIcon /> {t("pages.settings.addRule")}
              </Button>
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              {rows.length === 0 && (
                <p className="py-6 text-center text-sm text-muted-foreground">
                  {t("pages.settings.noRules")}
                </p>
              )}
              {rows.map((row, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: editable rows are positional
                <div key={i} className="flex items-center gap-2">
                  <Input
                    className="font-mono-logs"
                    placeholder={t("pages.settings.fieldPlaceholder")}
                    value={row.field}
                    onChange={(e) =>
                      setRows((prev) =>
                        prev.map((r, j) => (j === i ? { ...r, field: e.target.value } : r)),
                      )
                    }
                  />
                  <Select
                    value={row.action}
                    onValueChange={(v) =>
                      setRows((prev) =>
                        prev.map((r, j) => (j === i ? { ...r, action: v ?? r.action } : r)),
                      )
                    }
                  >
                    <SelectTrigger className="w-36">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ACTIONS.map((a) => (
                        <SelectItem key={a} value={a}>
                          {a}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t("pages.settings.removeRule")}
                    onClick={() => setRows((prev) => prev.filter((_, j) => j !== i))}
                  >
                    <Trash2Icon className="text-destructive" />
                  </Button>
                </div>
              ))}
            </CardContent>
          </Card>
        </div>
      </DataState>
    </>
  );
}
