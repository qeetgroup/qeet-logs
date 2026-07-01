import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input } from "@qeetrix/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { DatabaseZapIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";

import { validateKey } from "@/lib/auth";

export const Route = createFileRoute("/sign-in")({ component: SignInPage });

function SignInPage() {
  const navigate = useNavigate();
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = key.trim();
    if (!trimmed) { setError("API key is required."); return; }

    setError("");
    setLoading(true);
    try {
      const ok = await validateKey(trimmed);
      if (ok) {
        navigate({ to: "/" });
      } else {
        setError("Invalid API key — check the value and try again.");
      }
    } catch {
      setError("Could not reach the Qeet Logs API. Is the query service running?");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <div className="w-full max-w-sm space-y-6">
        {/* Brand */}
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex size-12 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-lg">
            <DatabaseZapIcon className="size-6" />
          </div>
          <div>
            <h1 className="font-heading text-xl font-semibold tracking-tight">Qeet Logs</h1>
            <p className="text-sm text-muted-foreground">Enterprise log management console</p>
          </div>
        </div>

        {/* Sign-in card */}
        <Card className="shadow-sm">
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Sign in with API key</CardTitle>
            <CardDescription>
              Create an API key with <code className="text-xs">logs:admin</code> scope to access
              the console. The key begins with <code className="text-xs">qeel_</code>.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-3">
              <div>
                <Input
                  id="api-key"
                  type="password"
                  autoComplete="current-password"
                  placeholder="qeel_…"
                  value={key}
                  onChange={(e) => { setKey(e.target.value); setError(""); }}
                  className="font-mono"
                  autoFocus
                />
                {error && <p className="mt-1.5 text-xs text-destructive">{error}</p>}
              </div>
              <Button type="submit" className="w-full" disabled={loading}>
                {loading && <Loader2Icon className="mr-1.5 size-4 animate-spin" />}
                Sign in
              </Button>
            </form>
          </CardContent>
        </Card>

        <p className="text-center text-xs text-muted-foreground">
          Your key is stored in <code>localStorage</code> and never sent to any server other
          than the configured Qeet Logs API.
        </p>
      </div>
    </div>
  );
}
