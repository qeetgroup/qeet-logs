import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  Input,
  ScrollArea,
  StatusPill,
} from "@qeetrix/ui";
import { createFileRoute } from "@tanstack/react-router";
import { PauseIcon, PlayIcon, RadioIcon, SquareIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/page-header";
import { wsURL } from "@/lib/api";
import { cell, formatDateTime, levelKind } from "@/lib/format";

export const Route = createFileRoute("/_app/tail")({ component: TailPage });

const DEFAULT_TAIL = "TAIL FROM logs";
const MAX_ROWS = 500;

type TailRow = Record<string, unknown>;
type ConnState = "idle" | "connecting" | "open" | "closed" | "error";

function TailPage() {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(DEFAULT_TAIL);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(false);
  const [paused, setPaused] = useState(false);
  const [state, setState] = useState<ConnState>("idle");
  const [rows, setRows] = useState<TailRow[]>([]);

  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  useEffect(() => {
    if (!active || !query.trim()) return;

    setState("connecting");
    let socket: WebSocket | null = null;
    try {
      socket = new WebSocket(wsURL("/v1/query/tail", { q: query }));
    } catch {
      setState("error");
      return;
    }

    socket.onopen = () => setState("open");
    socket.onerror = () => setState("error");
    socket.onclose = () => setState((s) => (s === "error" ? s : "closed"));
    socket.onmessage = (ev) => {
      if (pausedRef.current) return;
      let parsed: TailRow;
      try {
        parsed = JSON.parse(ev.data as string) as TailRow;
      } catch {
        parsed = { body: String(ev.data) };
      }
      setRows((prev) => {
        const next = [parsed, ...prev];
        return next.length > MAX_ROWS ? next.slice(0, MAX_ROWS) : next;
      });
    };

    return () => {
      socket?.close();
    };
  }, [active, query]);

  const start = useCallback(() => {
    setRows([]);
    setPaused(false);
    setQuery(draft);
    setActive(true);
  }, [draft]);

  const stop = useCallback(() => {
    setActive(false);
    setState("idle");
  }, []);

  const stateBadge: Record<
    ConnState,
    { kind: Parameters<typeof StatusPill>[0]["kind"]; label: string }
  > = {
    idle: { kind: "muted", label: t("pages.tail.stateIdle") },
    connecting: { kind: "warning", label: t("pages.tail.stateConnecting") },
    open: { kind: "success", label: t("pages.tail.stateOpen") },
    closed: { kind: "muted", label: t("pages.tail.stateClosed") },
    error: { kind: "danger", label: t("pages.tail.stateError") },
  };

  return (
    <>
      <PageHeader
        title={t("pages.tail.title")}
        description={t("pages.tail.description")}
        actions={<StatusPill kind={stateBadge[state].kind}>{stateBadge[state].label}</StatusPill>}
      />

      <Card>
        <CardContent className="flex flex-col gap-3 pt-6 sm:flex-row sm:items-center">
          <Input
            value={draft}
            spellCheck={false}
            className="font-mono-logs text-sm"
            aria-label={t("pages.tail.queryAria")}
            placeholder="TAIL FROM logs WHERE service = &quot;checkout&quot;"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") start();
            }}
          />
          <div className="flex shrink-0 items-center gap-2">
            {!active ? (
              <Button onClick={start} disabled={!draft.trim()}>
                <PlayIcon /> {t("pages.tail.start")}
              </Button>
            ) : (
              <>
                <Button variant="outline" onClick={() => setPaused((p) => !p)}>
                  {paused ? <PlayIcon /> : <PauseIcon />}
                  {paused ? t("pages.tail.resume") : t("pages.tail.pause")}
                </Button>
                <Button variant="destructive" onClick={stop}>
                  <SquareIcon /> {t("pages.tail.stop")}
                </Button>
              </>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-sm">
            <RadioIcon className="size-4 text-muted-foreground" />
            {t("pages.tail.stream")}
            <Badge variant="muted">{rows.length}</Badge>
          </CardTitle>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setRows([])}
            disabled={rows.length === 0}
          >
            <Trash2Icon /> {t("actions.clear")}
          </Button>
        </CardHeader>
        <CardContent>
          {rows.length === 0 ? (
            <EmptyState
              icon={RadioIcon}
              title={active ? t("pages.tail.waitingTitle") : t("pages.tail.notStreamingTitle")}
              description={
                active
                  ? t("pages.tail.waitingDescription")
                  : t("pages.tail.notStreamingDescription")
              }
            />
          ) : (
            <ScrollArea className="h-[60vh] w-full rounded-md border">
              <ul className="divide-y">
                {rows.map((r, i) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: streamed rows have no stable id
                  <li key={i} className="flex items-start gap-3 px-3 py-2 text-xs">
                    <span className="shrink-0 text-muted-foreground tabular-nums">
                      {formatDateTime((r.timestamp ?? r.ts ?? r.time) as string)}
                    </span>
                    <StatusPill kind={levelKind(cell(r.level ?? r.severity))} dot>
                      {cell(r.level ?? r.severity) || "log"}
                    </StatusPill>
                    {r.service ? (
                      <Badge variant="outline" className="shrink-0">
                        {cell(r.service)}
                      </Badge>
                    ) : null}
                    <span className="font-mono-logs min-w-0 flex-1 break-all">
                      {cell(r.body ?? r.message ?? r)}
                    </span>
                  </li>
                ))}
              </ul>
            </ScrollArea>
          )}
        </CardContent>
      </Card>
    </>
  );
}
