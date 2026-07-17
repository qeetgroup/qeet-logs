// Shared response shapes for the identity-aware / incident-intelligence
// endpoints of the query API. Several of these are Phase-2 surfaces on the
// backend roadmap; the console models the documented contract and degrades
// gracefully (empty/'not available yet' states) when an endpoint isn't live.

export type Severity = "critical" | "high" | "medium" | "low" | "info";

export type Incident = {
  id: string;
  title?: string;
  service?: string;
  severity?: Severity | string;
  status?: string;
  summary?: string;
  fingerprint?: string;
  count?: number;
  opened_at?: string;
  updated_at?: string;
  first_seen?: string;
  last_seen?: string;
};

export type Change = {
  id: string;
  kind?: string; // deploy | config | flag | infra
  service?: string;
  summary?: string;
  version?: string;
  actor?: string;
  status?: string;
  created_at?: string;
  at?: string;
};

/** RCA (root-cause analysis) summary for a service — GET /v1/rca?service= */
export type Rca = {
  service?: string;
  summary?: string;
  confidence?: number;
  hypotheses?: Array<{ title?: string; detail?: string; confidence?: number }>;
  correlated_logs?: Array<Record<string, unknown>>;
  suspected_deploy?: string;
};

/** Deploy culprit ranking — GET /v1/deploy/culprits?service= */
export type DeployCulprit = {
  deploy_id?: string;
  version?: string;
  service?: string;
  deployed_at?: string;
  score?: number;
  reason?: string;
  actor?: string;
};

/** Business impact context for an incident — GET /v1/incidents/{id}/context */
export type IncidentContext = {
  incident_id?: string;
  affected_customers?: number;
  affected_revenue?: number;
  currency?: string;
  tier?: string;
  regions?: string[];
  sla_breach?: boolean;
  summary?: string;
};

export type FeedbackVerdict = "actionable" | "noise";
