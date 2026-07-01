import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@qeetrix/ui";
import { ChevronsUpDownIcon, KeyRoundIcon, LogOutIcon } from "lucide-react";
import { keyStore } from "@/lib/api";
import { useNavigate } from "@tanstack/react-router";

function keyPrefix(): string {
  const k = keyStore.get() ?? "";
  return k.slice(0, 12) + "…";
}

export function NavUser() {
  const { isMobile } = useSidebar();
  const navigate = useNavigate();

  function handleSignOut() {
    keyStore.clear();
    navigate({ to: "/sign-in", replace: true });
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
              />
            }
          >
            <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
              <KeyRoundIcon className="size-4" />
            </div>
            <div className="grid flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">API Key</span>
              <span className="truncate text-xs text-muted-foreground font-mono">{keyPrefix()}</span>
            </div>
            <ChevronsUpDownIcon className="ms-auto size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuLabel className="text-xs text-muted-foreground">Authenticated as</DropdownMenuLabel>
            <DropdownMenuLabel className="font-mono text-xs truncate">{keyPrefix()}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleSignOut}>
              <LogOutIcon className="mr-2 size-4" />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
