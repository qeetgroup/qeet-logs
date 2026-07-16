import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
} from "@qeetrix/ui";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { DownloadIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { PageHeader } from "@/components/page-header";
import { API_BASE_URL } from "@/env";
import { keyStore } from "@/lib/api";
import { downloadText } from "@/lib/format";

export const Route = createFileRoute("/_app/export")({ component: ExportPage });

type Format = "csv" | "ndjson" | "json";

const EXT: Record<Format, string> = { csv: "csv", ndjson: "ndjson", json: "json" };
const MIME: Record<Format, string> = {
  csv: "text/csv",
  ndjson: "application/x-ndjson",
  json: "application/json",
};

function ExportPage() {
  const { t } = useTranslation();
  const [q, setQ] = useState("SELECT timestamp, service, level, body FROM logs LIMIT 10000");
  const [format, setFormat] = useState<Format>("csv");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  // The export stream can be CSV/NDJSON, so this bypasses the JSON `api()`
  // client and downloads the raw response with the key attached as a header.
  const run = useMutation({
    mutationFn: async () => {
      const base = API_BASE_URL.endsWith("/") ? API_BASE_URL : `${API_BASE_URL}/`;
      const url = new URL("v1/export", base);
      url.searchParams.set("q", q);
      url.searchParams.set("format", format);
      if (from) url.searchParams.set("from", from);
      if (to) url.searchParams.set("to", to);
      const key = keyStore.get();
      const res = await fetch(url, {
        headers: { "X-Qeet-Api-Key": key ?? "", Accept: "*/*" },
      });
      if (!res.ok) {
        let msg = `Export failed (${res.status})`;
        try {
          const body = (await res.json()) as { error?: string };
          if (body?.error) msg = body.error;
        } catch {
          /* non-JSON error body */
        }
        throw new Error(msg);
      }
      return res.text();
    },
    onSuccess: (text) => {
      downloadText(`qeet-logs-export.${EXT[format]}`, text, MIME[format]);
      toast.success(t("pages.export.downloaded"));
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t("pages.export.failed")),
    meta: { silent: true },
  });

  return (
    <>
      <PageHeader title={t("pages.export.title")} description={t("pages.export.description")} />

      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle className="text-sm">{t("pages.export.cardTitle")}</CardTitle>
          <CardDescription>{t("pages.export.cardDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="ex-query">{t("pages.export.queryLabel")}</Label>
            <Textarea
              id="ex-query"
              rows={4}
              className="font-mono-logs text-sm"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="flex flex-col gap-2">
              <Label>{t("pages.export.format")}</Label>
              <Select value={format} onValueChange={(v) => setFormat(v as Format)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="csv">CSV</SelectItem>
                  <SelectItem value="ndjson">NDJSON</SelectItem>
                  <SelectItem value="json">JSON</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="ex-from">{t("pages.export.fromOptional")}</Label>
              <Input
                id="ex-from"
                type="datetime-local"
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="ex-to">{t("pages.export.toOptional")}</Label>
              <Input
                id="ex-to"
                type="datetime-local"
                value={to}
                onChange={(e) => setTo(e.target.value)}
              />
            </div>
          </div>
          <div>
            <Button onClick={() => run.mutate()} disabled={!q.trim() || run.isPending}>
              {run.isPending ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
              {t("pages.export.download")}
            </Button>
          </div>
        </CardContent>
      </Card>
    </>
  );
}
