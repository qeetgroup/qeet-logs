import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@qeetrix/ui";
import { KeyRoundIcon, LogOutIcon } from "lucide-react";
import { keyStore } from "@/lib/api";
import { useNavigate } from "@tanstack/react-router";

export function HeaderUser() {
  const navigate = useNavigate();

  function handleSignOut() {
    keyStore.clear();
    navigate({ to: "/sign-in", replace: true });
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" className="rounded-full" aria-label="Account menu" />
        }
      >
        <div className="grid size-8 place-items-center rounded-full bg-primary/10 text-primary">
          <KeyRoundIcon className="size-4" />
        </div>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="min-w-52 rounded-lg">
        <DropdownMenuLabel className="text-xs text-muted-foreground">API Key</DropdownMenuLabel>
        <DropdownMenuLabel className="font-mono text-xs truncate">
          {(keyStore.get() ?? "").slice(0, 16)}…
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleSignOut}>
          <LogOutIcon className="mr-2 size-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
