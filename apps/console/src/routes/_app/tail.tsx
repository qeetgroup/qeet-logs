import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Field,
  FieldGroup,
  FieldLabel,
  Input,
  PresenceIndicator,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@qeetrix/ui";
import { createFileRoute } from "@tanstack/react-router";
import { PauseIcon, PlayIcon, Trash2Icon, WifiIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { keyStore, wsURL } from "@/lib/api";

export const Route = createFileRoute("/_app/tail")({ component: TailPage });

type LogEntry = {
  id: string;
  timestamp: string;
  level: string;
  service: string;
  message: string;
  raw: string;
};

const LEVEL_COLORS: Record<string, string> = {
  error: "bg-destructive/10 text-destructive border-destructive/30",
  warn: "bg-warning/10 text-warning border-warning/30",
  warning: "bg-warning/10 text-warning border-warning/30",
  info: "bg-info/10 text-info border-info/30",
  debug: "bg-muted text-muted-foreground",
  trace: "bg-muted text-muted-foreground",
};

function levelCls(level: string) {
  return LEVEL_COLORS[level?.toLowerCase()] ?? "bg-muted text-muted-foreground";
}

type WsStatus = "idle" | "connecting" | "connected" | "disconnected" | "error";

function TailPage() {
  const [service, setService] = useState("");
  const [level, setLevel] = useState("all");
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [status, setStatus] = useState<WsStatus>("idle");
  const [paused, setPaused] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const pausedRef = useRef(false);

  pausedRef.current = paused;

  const connect = useCallback(() => {
    if (wsRef.current) wsRef.current.close();

    const levelFilter = level !== "all" ? ` AND level = '${level}'` : "";
    const serviceFilter = service ? ` AND service = '${service}'` : "";
    const q = `TAIL FROM logs WHERE 1=1${serviceFilter}${levelFilter}`;

    const key = keyStore.get() ?? "";
    const url = wsURL("/v1/query/tail", { q });

    // Append the api key as a query param since WS can't send custom headers from the browser.
    const fullUrl = `${url}&X-Qeet-Api-Key=${encodeURIComponent(key)}`;

    setStatus("connecting");
    const ws = new WebSocket(fullUrl);
    wsRef.current = ws;

    ws.onopen = () => setStatus("connected");
    ws.onerror = () => setStatus("error");
    ws.onclose = () => setStatus((s) => (s === "connected" ? "disconnected" : s));

    ws.onmessage = (ev) => {
      if (pausedRef.current) return;
      try {
        const data = JSON.parse(ev.data as string) as Record<string, unknown>;
        const entry: LogEntry = {
          id: String(data.id ?? Date.now()),
          timestamp: String(data.timestamp ?? new Date().toISOString()),
          level: String(data.level ?? "info"),
          service: String(data.service ?? "—"),
          message: String(data.message ?? JSON.stringify(data)),
          raw: ev.data as string,
        };
        setLogs((prev) => [...prev.slice(-499), entry]);
      } catch {
        // Non-JSON frame — ignore
      }
    };
  }, [service, level]);

  function disconnect() {
    wsRef.current?.close();
    wsRef.current = null;
    setStatus("disconnected");
  }

  function clear() {
    setLogs([]);
  }

  // Auto-scroll to bottom when not paused
  useEffect(() => {
    if (!paused) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [logs, paused]);

  // Cleanup on unmount
  useEffect(() => () => { wsRef.current?.close(); }, []);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Live Tail</h1>
          <p className="text-sm text-muted-foreground">Stream log events in real time via WebSocket</p>
        </div>
        <div className="flex items-center gap-1.5">
          <PresenceIndicator
            status={
              status === "connected" ? "online"
              : status === "connecting" ? "busy"
              : "offline"
            }
            size="sm"
            pulse={status === "connected"}
          />
          <span className="text-xs capitalize text-muted-foreground">{status}</span>
        </div>
      </div>

      {/* Controls */}
      <Card>
        <CardContent className="py-4">
          <FieldGroup className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="tail-service">Service</FieldLabel>
              <Input
                id="tail-service"
                placeholder="e.g. payments"
                value={service}
                onChange={(e) => setService(e.target.value)}
                disabled={status === "connected"}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="tail-level">Min Level</FieldLabel>
              <Select value={level} onValueChange={(v) => v != null && setLevel(v)} disabled={status === "connected"}>
                <SelectTrigger id="tail-level">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All levels</SelectItem>
                  <SelectItem value="debug">Debug+</SelectItem>
                  <SelectItem value="info">Info+</SelectItem>
                  <SelectItem value="warn">Warn+</SelectItem>
                  <SelectItem value="error">Error only</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>&nbsp;</FieldLabel>
              <div className="flex gap-2">
                {status !== "connected" ? (
                  <Button className="flex-1" onClick={connect}>
                    <WifiIcon className="mr-1.5 size-4" /> Connect
                  </Button>
                ) : (
                  <Button variant="destructive" className="flex-1" onClick={disconnect}>
                    Disconnect
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => setPaused((p) => !p)}
                  aria-label={paused ? "Resume" : "Pause"}
                  title={paused ? "Resume streaming" : "Pause streaming"}
                >
                  {paused ? <PlayIcon className="size-4" /> : <PauseIcon className="size-4" />}
                </Button>
                <Button variant="outline" size="icon" onClick={clear} aria-label="Clear">
                  <Trash2Icon className="size-4" />
                </Button>
              </div>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      {/* Log stream */}
      <Card className="flex-1">
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <CardTitle className="text-base">
              Stream
              {logs.length > 0 && (
                <span className="ml-2 text-sm font-normal text-muted-foreground">
                  {logs.length} events
                </span>
              )}
            </CardTitle>
            {paused && (
              <Badge variant="outline" className="border-warning/40 bg-warning/10 text-warning text-xs">
                Paused
              </Badge>
            )}
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <div
            className="h-[520px] overflow-y-auto bg-muted/20 font-mono text-xs"
            aria-live="polite"
            aria-atomic={false}
          >
            {logs.length === 0 ? (
              <div className="flex h-full items-center justify-center text-muted-foreground">
                {status === "idle"
                  ? "Click Connect to start streaming logs."
                  : status === "connecting"
                  ? "Connecting…"
                  : "No events yet — waiting for logs."}
              </div>
            ) : (
              <table className="w-full border-collapse">
                <tbody>
                  {logs.map((log) => (
                    <tr
                      key={log.id}
                      className={`border-b border-border/30 hover:bg-muted/40 transition-colors ${levelCls(log.level)}`}
                    >
                      <td className="w-44 shrink-0 whitespace-nowrap px-3 py-1.5 text-muted-foreground">
                        {log.timestamp.slice(0, 19).replace("T", " ")}
                      </td>
                      <td className="w-14 px-2 py-1.5">
                        <span className="uppercase font-semibold tracking-widest text-[10px]">
                          {log.level.slice(0, 4)}
                        </span>
                      </td>
                      <td className="w-32 truncate px-2 py-1.5 text-muted-foreground">
                        {log.service}
                      </td>
                      <td className="px-2 py-1.5 break-all">
                        {log.message}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            <div ref={bottomRef} />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
