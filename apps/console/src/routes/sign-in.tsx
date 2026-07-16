import {
  Alert,
  AlertDescription,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
} from "@qeetrix/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { KeyRoundIcon, Loader2Icon, ScrollTextIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { isAuthenticated, useSignIn } from "@/lib/auth";

export const Route = createFileRoute("/sign-in")({ component: SignInPage });

function SignInPage() {
  const navigate = useNavigate();
  const [key, setKey] = useState("");
  const signIn = useSignIn();

  useEffect(() => {
    if (isAuthenticated()) navigate({ to: "/", replace: true });
  }, [navigate]);

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted/30 p-6">
      <div className="flex items-center gap-2 font-heading text-lg font-semibold">
        <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <ScrollTextIcon className="size-4" />
        </span>
        Qeet Logs
      </div>

      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>
            Paste a Qeet Logs API key with the <code>logs:admin</code> scope to open the console.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (key.trim()) signIn.mutate(key);
            }}
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="api-key">API key</Label>
              <div className="relative">
                <KeyRoundIcon className="pointer-events-none absolute inset-s-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="api-key"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="qlog_live_…"
                  className="ps-9 font-mono-logs"
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                />
              </div>
            </div>

            {signIn.isError && (
              <Alert variant="danger">
                <AlertDescription>
                  {signIn.error instanceof Error
                    ? signIn.error.message
                    : "That API key could not be verified."}
                </AlertDescription>
              </Alert>
            )}

            <Button type="submit" disabled={!key.trim() || signIn.isPending} className="w-full">
              {signIn.isPending && <Loader2Icon className="animate-spin" />}
              {signIn.isPending ? "Verifying…" : "Continue"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <p className="max-w-sm text-center text-xs text-muted-foreground">
        Keys are stored in this browser only and sent as the <code>X-Qeet-Api-Key</code> header.
        Mint one with <code>POST /v1/admin/api-keys</code> or the seed script.
      </p>
    </div>
  );
}
