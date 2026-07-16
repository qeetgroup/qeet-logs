import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CopyableSecret,
  DataState,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  Input,
  Label,
  StatusPill,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { KeyRoundIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useConfirmDialog } from "@/components/confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";
import { formatDateTime, relativeTime } from "@/lib/format";

export const Route = createFileRoute("/_app/api-keys")({ component: ApiKeysPage });

// The Go handler marshals these structs without json tags → PascalCase keys.
type ApiKeyRow = {
  ID: string;
  Name: string;
  KeyPrefix: string;
  Scopes: string[];
  LastUsedAt?: string | null;
  ExpiresAt?: string | null;
  RevokedAt?: string | null;
  CreatedAt: string;
};

type NewApiKey = ApiKeyRow & { Key: string };

const ALL_SCOPES = [
  "logs:ingest",
  "logs:read",
  "logs:query",
  "logs:export",
  "logs:admin",
  "logs:platform",
] as const;

function ApiKeysPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [dialog, confirm] = useConfirmDialog();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Set<string>>(new Set(["logs:read", "logs:query"]));
  const [created, setCreated] = useState<NewApiKey | null>(null);

  const keysQ = useQuery({
    queryKey: ["api-keys"],
    queryFn: () => api<ApiKeyRow[]>("/v1/admin/api-keys"),
    retry: false,
    meta: { silent: true },
  });

  const createKey = useMutation({
    mutationFn: (body: { name: string; scopes: string[] }) =>
      api<NewApiKey>("/v1/admin/api-keys", { method: "POST", body }),
    onSuccess: (key) => {
      qc.invalidateQueries({ queryKey: ["api-keys"] });
      setOpen(false);
      setName("");
      setScopes(new Set(["logs:read", "logs:query"]));
      setCreated(key);
    },
    meta: { successMessage: t("pages.apiKeys.createdToast") },
  });

  const revokeKey = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/api-keys/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["api-keys"] }),
    meta: { successMessage: t("pages.apiKeys.revokedToast") },
  });

  function toggleScope(s: string) {
    setScopes((prev) => {
      const next = new Set(prev);
      if (next.has(s)) next.delete(s);
      else next.add(s);
      return next;
    });
  }

  const keys = keysQ.data ?? [];

  return (
    <>
      <PageHeader
        title={t("pages.apiKeys.title")}
        description={t("pages.apiKeys.description")}
        actions={
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> {t("pages.apiKeys.newKey")}
          </Button>
        }
      />

      <Card>
        <CardContent className="p-0">
          <DataState
            isLoading={keysQ.isLoading}
            isError={keysQ.isError}
            error={keysQ.error}
            isEmpty={!keysQ.isLoading && keys.length === 0}
            empty={
              <EmptyState
                icon={KeyRoundIcon}
                title={t("pages.apiKeys.emptyTitle")}
                description={t("pages.apiKeys.emptyDescription")}
                action={
                  <Button onClick={() => setOpen(true)}>
                    <PlusIcon /> {t("pages.apiKeys.newKey")}
                  </Button>
                }
              />
            }
            skeletonRows={5}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.name")}</TableHead>
                  <TableHead>{t("columns.prefix")}</TableHead>
                  <TableHead>{t("columns.scopes")}</TableHead>
                  <TableHead>{t("columns.lastUsed")}</TableHead>
                  <TableHead>{t("columns.status")}</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => {
                  const revoked = Boolean(k.RevokedAt);
                  return (
                    <TableRow key={k.ID}>
                      <TableCell className="font-medium">{k.Name}</TableCell>
                      <TableCell className="font-mono-logs text-xs text-muted-foreground">
                        {k.KeyPrefix}…
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {(k.Scopes ?? []).map((s) => (
                            <Badge key={s} variant="muted" className="font-mono-logs text-[10px]">
                              {s}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {k.LastUsedAt ? relativeTime(k.LastUsedAt) : t("pages.apiKeys.never")}
                      </TableCell>
                      <TableCell>
                        <StatusPill kind={revoked ? "danger" : "success"}>
                          {revoked ? t("pages.apiKeys.revoked") : t("pages.apiKeys.active")}
                        </StatusPill>
                      </TableCell>
                      <TableCell>
                        {!revoked && (
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("pages.apiKeys.revokeAria", { name: k.Name })}
                            onClick={() =>
                              confirm({
                                title: t("pages.apiKeys.revokeTitle", { name: k.Name }),
                                description: t("pages.apiKeys.revokeDescription"),
                                confirmLabel: t("pages.apiKeys.revoke"),
                                onConfirm: () => revokeKey.mutate(k.ID),
                              })
                            }
                          >
                            <Trash2Icon className="text-destructive" />
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </DataState>
        </CardContent>
      </Card>

      {/* Create dialog */}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.apiKeys.createTitle")}</DialogTitle>
            <DialogDescription>{t("pages.apiKeys.createDescription")}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="key-name">{t("fields.name")}</Label>
              <Input
                id="key-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("pages.apiKeys.namePlaceholder")}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label>{t("fields.scopes")}</Label>
              <div className="flex flex-wrap gap-2">
                {ALL_SCOPES.map((s) => {
                  const on = scopes.has(s);
                  return (
                    <Button
                      key={s}
                      type="button"
                      size="sm"
                      variant={on ? "default" : "outline"}
                      className="font-mono-logs text-xs"
                      onClick={() => toggleScope(s)}
                    >
                      {s}
                    </Button>
                  );
                })}
              </div>
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button variant="outline">{t("actions.cancel")}</Button>} />
            <Button
              onClick={() => createKey.mutate({ name: name.trim(), scopes: [...scopes] })}
              disabled={!name.trim() || scopes.size === 0 || createKey.isPending}
            >
              {createKey.isPending && <Loader2Icon className="animate-spin" />}
              {t("pages.apiKeys.createKey")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reveal-once dialog */}
      <Dialog open={!!created} onOpenChange={(o) => !o && setCreated(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.apiKeys.revealTitle")}</DialogTitle>
            <DialogDescription>{t("pages.apiKeys.revealDescription")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-3">
            <Alert variant="warning">
              <AlertTitle>{created?.Name}</AlertTitle>
              <AlertDescription>
                {t("fields.scopes")}: {(created?.Scopes ?? []).join(", ")}
                {created?.ExpiresAt
                  ? ` · ${t("pages.apiKeys.expires", { date: formatDateTime(created.ExpiresAt) })}`
                  : ""}
              </AlertDescription>
            </Alert>
            {created?.Key && <CopyableSecret value={created.Key} oneLine />}
          </div>
          <DialogFooter>
            <DialogClose render={<Button>{t("actions.done")}</Button>} />
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {dialog}
    </>
  );
}
