import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Checkbox,
  CopyableSecret,
  EmptyState,
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  Input,
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TimeSince,
} from "@qeetrix/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { KeyRoundIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";

import { api } from "@/lib/api";

export const Route = createFileRoute("/_app/api-keys")({ component: APIKeysPage });

const ALL_SCOPES = [
  { value: "logs:ingest", label: "Ingest", description: "Write log events" },
  { value: "logs:read", label: "Read", description: "Query logs (read-only)" },
  { value: "logs:query", label: "Query", description: "Advanced query access" },
  { value: "logs:export", label: "Export", description: "Download / export" },
  { value: "logs:admin", label: "Admin", description: "Manage keys, rules, settings" },
  { value: "logs:platform", label: "Platform", description: "Cross-tenant access (operators only)" },
] as const;

type APIKey = {
  ID: string;
  TenantID: string;
  Name: string;
  KeyPrefix: string;
  Scopes: string[];
  LastUsedAt: string | null;
  ExpiresAt: string | null;
  RevokedAt: string | null;
  CreatedAt: string;
};

type NewAPIKey = APIKey & { Key: string };

function useAPIKeys() {
  return useQuery({
    queryKey: ["api-keys"],
    queryFn: () => api<APIKey[]>("/v1/admin/api-keys"),
  });
}

function CreateSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient();
  const [selectedScopes, setSelectedScopes] = useState<string[]>(["logs:ingest", "logs:read"]);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [nameError, setNameError] = useState("");

  const createM = useMutation({
    mutationFn: (data: { name: string; scopes: string[] }) =>
      api<NewAPIKey>("/v1/admin/api-keys", { method: "POST", body: data }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["api-keys"] });
      setNewKey(res.Key);
    },
    meta: { successMessage: "API key created — save it now" },
  });

  function toggleScope(scope: string) {
    setSelectedScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope],
    );
  }

  function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const name = String(fd.get("name") ?? "").trim();
    if (!name) { setNameError("Name is required."); return; }
    if (selectedScopes.length === 0) return;
    setNameError("");
    createM.mutate({ name, scopes: selectedScopes });
  }

  function handleClose() {
    setNewKey(null);
    setSelectedScopes(["logs:ingest", "logs:read"]);
    setNameError("");
    onOpenChange(false);
  }

  return (
    <Sheet open={open} onOpenChange={handleClose}>
      <SheetContent side="right" className="w-full sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>Create API Key</SheetTitle>
          <SheetDescription>
            The raw key is shown once on creation — copy it now.
          </SheetDescription>
        </SheetHeader>

        {newKey ? (
          /* Revealed key view */
          <div className="flex h-full flex-col gap-4 px-4 py-4">
            <div className="rounded-lg border border-success/40 bg-success/5 p-4">
              <p className="mb-3 text-sm font-medium text-success">Save your API key now</p>
              <CopyableSecret value={newKey} label="API Key" />
              <p className="mt-2 text-xs text-muted-foreground">
                This key will <strong>not</strong> be shown again. Store it securely.
              </p>
            </div>
            <SheetFooter>
              <Button onClick={handleClose} className="w-full">Done</Button>
            </SheetFooter>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="flex h-full flex-col">
            <div className="flex-1 overflow-y-auto px-4 py-3">
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="key-name">Name</FieldLabel>
                  <Input id="key-name" name="name" placeholder="CI pipeline key" />
                  {nameError && <FieldError>{nameError}</FieldError>}
                </Field>
                <Field>
                  <FieldLabel>Scopes</FieldLabel>
                  <FieldDescription>Grant only the permissions this key needs.</FieldDescription>
                  <div className="mt-2 space-y-2">
                    {ALL_SCOPES.map(({ value, label, description }) => (
                      <label key={value} className="flex cursor-pointer items-start gap-3 rounded-lg border p-3 hover:bg-muted/40">
                        <Checkbox
                          checked={selectedScopes.includes(value)}
                          onCheckedChange={() => toggleScope(value)}
                          id={`scope-${value}`}
                        />
                        <div>
                          <p className="text-sm font-medium leading-none">{label}</p>
                          <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
                        </div>
                      </label>
                    ))}
                  </div>
                </Field>
              </FieldGroup>
            </div>
            <SheetFooter className="border-t px-4 py-3">
              <SheetClose render={<Button variant="outline" type="button" />}>Cancel</SheetClose>
              <Button type="submit" disabled={createM.isPending || selectedScopes.length === 0}>
                {createM.isPending && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
                Create Key
              </Button>
            </SheetFooter>
          </form>
        )}
      </SheetContent>
    </Sheet>
  );
}

function APIKeysPage() {
  const keysQ = useAPIKeys();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);

  const revokeM = useMutation({
    mutationFn: (id: string) => api(`/v1/admin/api-keys/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["api-keys"] });
      setRevoking(null);
    },
    meta: { successMessage: "API key revoked" },
  });

  const keys = keysQ.data ?? [];

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">API Keys</h1>
          <p className="text-sm text-muted-foreground">Scoped keys for SDK and CLI access</p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <PlusIcon className="mr-1.5 size-4" /> New Key
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Active Keys</CardTitle>
          <CardDescription>
            {keys.length} active key{keys.length !== 1 ? "s" : ""}
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {keysQ.isLoading ? (
            <div className="px-6 py-12 text-center">
              <Loader2Icon className="mx-auto size-6 animate-spin text-muted-foreground" />
            </div>
          ) : keys.length === 0 ? (
            <div className="px-6 py-12">
              <EmptyState
                icon={KeyRoundIcon}
                title="No API keys"
                description="Create a key to start ingesting logs or querying via the API."
              >
                <Button onClick={() => setCreating(true)}>
                  <PlusIcon className="mr-1.5 size-4" /> Create first key
                </Button>
              </EmptyState>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Key prefix</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((key) => (
                  <TableRow key={key.ID}>
                    <TableCell className="font-medium">{key.Name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {key.KeyPrefix}…
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {key.Scopes.map((s) => (
                          <Badge key={s} variant="secondary" className="text-[10px]">
                            {s.replace("logs:", "")}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {key.LastUsedAt ? <TimeSince value={key.LastUsedAt} /> : "Never"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {key.ExpiresAt ? <TimeSince value={key.ExpiresAt} /> : "Never"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      <TimeSince value={key.CreatedAt} />
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => setRevoking(key.ID)}
                        aria-label="Revoke key"
                      >
                        <Trash2Icon className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <CreateSheet open={creating} onOpenChange={setCreating} />

      <AlertDialog open={!!revoking} onOpenChange={(o) => !o && setRevoking(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke this API key?</AlertDialogTitle>
            <AlertDialogDescription>
              All requests using this key will immediately start failing. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              variant="destructive"
              onClick={() => revoking && revokeM.mutate(revoking)}
              disabled={revokeM.isPending}
            >
              {revokeM.isPending && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
              Revoke Key
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
