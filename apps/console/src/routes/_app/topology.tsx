import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@qeetrix/ui";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Loader2Icon, NetworkIcon, RefreshCwIcon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/topology")({ component: TopologyPage });

type TopoNode = {
  service: string;
  spans: number;
  errors: number;
  log_count: number;
  coverage: "full" | "traces-only" | "logs-only";
};

type TopoEdge = {
  caller: string;
  callee: string;
  calls: number;
  errors: number;
  p95_ms: number;
};

type TopoGraph = {
  window_seconds: number;
  nodes: TopoNode[];
  edges: TopoEdge[];
  focus?: string;
  blast_radius?: string[];
};

const COVERAGE_VARIANT: Record<string, "secondary" | "outline" | "destructive"> = {
  full: "secondary",
  "traces-only": "outline",
  "logs-only": "destructive",
};

function TopologyPage() {
  const [focus, setFocus] = useState<string>("");

  const graphQ = useQuery({
    queryKey: ["topology", focus],
    queryFn: () =>
      api<TopoGraph>("/v1/topology", { query: focus ? { service: focus } : {} }),
  });

  const graph = graphQ.data;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">Service Topology</h1>
          <p className="text-sm text-muted-foreground">
            Dependency graph derived from traces + log service inference
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => graphQ.refetch()} disabled={graphQ.isFetching}>
          <RefreshCwIcon className={`mr-1.5 size-4 ${graphQ.isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {focus && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Blast radius — {focus}</CardTitle>
            <CardDescription>Services affected if {focus} fails</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            {(graph?.blast_radius ?? []).length === 0 ? (
              <span className="text-sm text-muted-foreground">No upstream dependents.</span>
            ) : (
              graph?.blast_radius?.map((s) => <Badge key={s} variant="destructive">{s}</Badge>)
            )}
            <Button variant="ghost" size="sm" onClick={() => setFocus("")}>Clear focus</Button>
          </CardContent>
        </Card>
      )}

      {graphQ.isLoading ? (
        <div className="flex items-center justify-center py-16 text-muted-foreground">
          <Loader2Icon className="mr-2 size-5 animate-spin" /> Deriving graph…
        </div>
      ) : !graph || graph.nodes.length === 0 ? (
        <EmptyState
          icon={<NetworkIcon />}
          title="No topology yet"
          description="Send OTLP traces to build the dependency graph."
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Services ({graph.nodes.length})</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Service</TableHead>
                    <TableHead>Coverage</TableHead>
                    <TableHead className="text-right">Spans</TableHead>
                    <TableHead className="text-right">Errors</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {graph.nodes.map((n) => (
                    <TableRow key={n.service} className="cursor-pointer" onClick={() => setFocus(n.service)}>
                      <TableCell className="font-medium">{n.service}</TableCell>
                      <TableCell><Badge variant={COVERAGE_VARIANT[n.coverage]}>{n.coverage}</Badge></TableCell>
                      <TableCell className="text-right tabular-nums">{n.spans}</TableCell>
                      <TableCell className="text-right tabular-nums">{n.errors}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Dependencies ({graph.edges.length})</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Caller</TableHead>
                    <TableHead>Callee</TableHead>
                    <TableHead className="text-right">Calls</TableHead>
                    <TableHead className="text-right">p95 (ms)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {graph.edges.map((e) => (
                    <TableRow key={`${e.caller}->${e.callee}`}>
                      <TableCell>{e.caller}</TableCell>
                      <TableCell className="font-medium">{e.callee}</TableCell>
                      <TableCell className="text-right tabular-nums">{e.calls}</TableCell>
                      <TableCell className="text-right tabular-nums">{e.p95_ms.toFixed(1)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
