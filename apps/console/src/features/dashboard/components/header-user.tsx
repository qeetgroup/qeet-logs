import {
  Avatar,
  AvatarFallback,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@qeetrix/ui";
import { Link } from "@tanstack/react-router";
import { KeyRoundIcon, Loader2Icon, LogOutIcon, Settings2Icon, WebhookIcon } from "lucide-react";

import { keyPrefix, useReadyz, useSignOut } from "@/lib/auth";

export function HeaderUser() {
  const signOut = useSignOut();
  const readyz = useReadyz();
  const prefix = keyPrefix() ?? "No key";
  const healthy = readyz.data?.healthy ?? readyz.isSuccess;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" className="rounded-full" aria-label="Account menu">
            <Avatar className="size-8">
              <AvatarFallback className="text-xs">
                <KeyRoundIcon className="size-4" />
              </AvatarFallback>
            </Avatar>
          </Button>
        }
      />
      <DropdownMenuContent align="end" sideOffset={8} className="min-w-64 rounded-lg">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="font-normal">
            <div className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">API key</span>
              <span className="truncate font-mono-logs text-xs text-muted-foreground">
                {prefix}
              </span>
              <span className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                <span
                  className={`inline-block size-2 rounded-full ${
                    healthy ? "bg-success" : "bg-destructive"
                  }`}
                  aria-hidden
                />
                Backend {healthy ? "healthy" : "unreachable"}
              </span>
            </div>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem render={<Link to="/api-keys" />}>
            <KeyRoundIcon />
            API Keys
          </DropdownMenuItem>
          <DropdownMenuItem render={<Link to="/webhooks" />}>
            <WebhookIcon />
            Webhooks
          </DropdownMenuItem>
          <DropdownMenuItem render={<Link to="/settings" />}>
            <Settings2Icon />
            Settings
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onClick={() => signOut.mutate()}
          disabled={signOut.isPending}
        >
          {signOut.isPending ? <Loader2Icon className="animate-spin" /> : <LogOutIcon />}
          {signOut.isPending ? "Signing out…" : "Sign out"}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
