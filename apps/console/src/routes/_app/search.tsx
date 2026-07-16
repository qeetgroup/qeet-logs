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
import { z } from "zod";

import { PageHeader } from "@/components/page-header";
import { ResultsTable } from "@/components/results-table";
import { api } from "@/lib/api";
import { downloadText, formatNumber, toCSV } from "@/lib/format";
import { useLogQuery } from "@/lib/query";

const searchSchema = z.object({ q: z.string().optional() });

export const Route = createFileRoute("/_app/search")({
  component: SearchPage,
  validateSearch: searchSchema,
});

const SAMPLE = 'SELECT timestamp, service, level, body FROM logs WHERE level = "error" LIMIT 100';

function SearchPage() {
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
    meta: { successMessage: "Search saved" },
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
        title="Log Search"
        description="Run LogQL++ against ClickHouse for the current tenant. Cmd/Ctrl+Enter to execute."
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => exportAs("csv")}
              disabled={!result.data || result.data.rows.length === 0}
            >
              <DownloadIcon /> Export CSV
            </Button>
            <Button
              variant="outline"
              onClick={() => setSaveOpen(true)}
              disabled={draft.trim().length === 0}
            >
              <BookmarkPlusIcon /> Save
            </Button>
          </>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Query</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Textarea
            value={draft}
            spellCheck={false}
            rows={4}
            className="font-mono-logs text-sm"
            aria-label="LogQL++ query"
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
              Press <Kbd>⌘</Kbd> <Kbd>↵</Kbd> to run. TAIL queries stream from the Live Tail page.
            </p>
            <Button onClick={run} disabled={draft.trim().length === 0 || result.isFetching}>
              {result.isFetching ? <Loader2Icon className="animate-spin" /> : <PlayIcon />}
              Run
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="text-sm">
            Results
            {result.data ? (
              <span className="ms-2 text-xs font-normal text-muted-foreground">
                {formatNumber(result.data.count)} rows
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
              Enter a query above and press Run to see results.
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
            <DialogTitle>Save search</DialogTitle>
            <DialogDescription>
              Store this LogQL++ statement so the team can re-run it from Saved Searches.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="save-name">Name</Label>
              <Input
                id="save-name"
                value={saveName}
                onChange={(e) => setSaveName(e.target.value)}
                placeholder="Errors in checkout (last hour)"
              />
            </div>
            <Textarea readOnly value={draft} rows={3} className="font-mono-logs text-xs" />
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">Cancel</Button>} />
            <Button
              onClick={() => saveSearch.mutate({ name: saveName.trim(), query_text: draft })}
              disabled={!saveName.trim() || saveSearch.isPending}
            >
              {saveSearch.isPending && <Loader2Icon className="animate-spin" />}
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
