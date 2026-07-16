import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Kbd,
  Label,
  Textarea,
} from "@qeetrix/ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { BookmarkPlusIcon, DownloadIcon, Loader2Icon, PlayIcon } from "lucide-react";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { z } from "zod";

import { PageHeader } from "@/components/page-header";
import { ResultsTable } from "@/components/results-table";
import { api } from "@/lib/api";
import { downloadText, toCSV } from "@/lib/format";
import { useLogQuery } from "@/lib/query";

const searchSchema = z.object({ q: z.string().optional() });

export const Route = createFileRoute("/_app/search")({
  component: SearchPage,
  validateSearch: searchSchema,
});

const SAMPLE = 'SELECT timestamp, service, level, body FROM logs WHERE level = "error" LIMIT 100';

function SearchPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const initial = Route.useSearch().q;
  const [draft, setDraft] = useState(initial ?? SAMPLE);
  const [submitted, setSubmitted] = useState(initial ?? "");
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState("");

  const result = useLogQuery(submitted, { enabled: submitted.trim().length > 0 });

  const saveSearch = useMutation({
    mutationFn: (input: { name: string; query_text: string }) =>
      api("/v1/admin/saved-searches", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["saved-searches"] });
      setSaveOpen(false);
      setSaveName("");
    },
    meta: { successMessage: t("pages.search.savedToast") },
  });

  function run() {
    setSubmitted(draft);
  }

  function exportAs(format: "csv" | "json") {
    if (!result.data) return;
    if (format === "csv") {
      downloadText("qeet-logs-query.csv", toCSV(result.data.columns, result.data.rows), "text/csv");
    } else {
      downloadText(
        "qeet-logs-query.json",
        JSON.stringify(result.data.rows, null, 2),
        "application/json",
      );
    }
  }

  return (
    <>
      <PageHeader
        title={t("pages.search.title")}
        description={t("pages.search.description")}
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => exportAs("csv")}
              disabled={!result.data || result.data.rows.length === 0}
            >
              <DownloadIcon /> {t("pages.search.exportCsv")}
            </Button>
            <Button
              variant="outline"
              onClick={() => setSaveOpen(true)}
              disabled={draft.trim().length === 0}
            >
              <BookmarkPlusIcon /> {t("pages.search.save")}
            </Button>
          </>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t("pages.search.queryLabel")}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Textarea
            value={draft}
            spellCheck={false}
            rows={4}
            className="font-mono-logs text-sm"
            aria-label={t("pages.search.queryAria")}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                run();
              }
            }}
          />
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              <Trans i18nKey="pages.search.runHint" components={{ kbd: <Kbd /> }} />
            </p>
            <Button onClick={run} disabled={draft.trim().length === 0 || result.isFetching}>
              {result.isFetching ? <Loader2Icon className="animate-spin" /> : <PlayIcon />}
              {t("actions.run")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="text-sm">
            {t("pages.search.results")}
            {result.data ? (
              <span className="ms-2 text-xs font-normal text-muted-foreground">
                {t("pages.search.rowsCount", { count: result.data.count })}
              </span>
            ) : null}
          </CardTitle>
          {result.data && result.data.rows.length > 0 && (
            <Button variant="ghost" size="sm" onClick={() => exportAs("json")}>
              <DownloadIcon /> JSON
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {submitted.trim().length === 0 ? (
            <p className="py-10 text-center text-sm text-muted-foreground">
              {t("pages.search.prompt")}
            </p>
          ) : (
            <ResultsTable
              data={result.data}
              isLoading={result.isFetching && !result.data}
              isError={result.isError}
              error={result.error}
            />
          )}
        </CardContent>
      </Card>

      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.search.saveTitle")}</DialogTitle>
            <DialogDescription>{t("pages.search.saveDescription")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="save-name">{t("fields.name")}</Label>
              <Input
                id="save-name"
                value={saveName}
                onChange={(e) => setSaveName(e.target.value)}
                placeholder={t("pages.search.saveNamePlaceholder")}
              />
            </div>
            <Textarea readOnly value={draft} rows={3} className="font-mono-logs text-xs" />
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">{t("actions.cancel")}</Button>} />
            <Button
              onClick={() => saveSearch.mutate({ name: saveName.trim(), query_text: draft })}
              disabled={!saveName.trim() || saveSearch.isPending}
            >
              {saveSearch.isPending && <Loader2Icon className="animate-spin" />}
              {t("actions.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
