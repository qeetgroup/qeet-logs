import {
  Avatar,
  AvatarFallback,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@qeetrix/ui";
import { Link } from "@tanstack/react-router";
import {
  ChevronsUpDownIcon,
  KeyRoundIcon,
  Loader2Icon,
  LogOutIcon,
  Settings2Icon,
} from "lucide-react";

import { keyPrefix, useReadyz, useSignOut } from "@/lib/auth";

export function NavUser() {
  const { isMobile } = useSidebar();
  const signOut = useSignOut();
  const readyz = useReadyz();
  const prefix = keyPrefix() ?? "No key";
  const healthy = readyz.data?.healthy ?? readyz.isSuccess;

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<SidebarMenuButton size="lg" className="aria-expanded:bg-muted" />}
          >
            <Avatar>
              <AvatarFallback>
                <KeyRoundIcon className="size-4" />
              </AvatarFallback>
            </Avatar>
            <div className="grid flex-1 text-start text-sm leading-tight">
              <span className="truncate font-medium">API key</span>
              <span className="truncate font-mono-logs text-xs text-muted-foreground">
                {prefix}
              </span>
            </div>
            <ChevronsUpDownIcon className="ms-auto size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col gap-0.5">
                  <span className="text-sm font-medium">Signed in with API key</span>
                  <span className="text-xs text-muted-foreground">
                    Backend: {healthy ? "healthy" : "unreachable"}
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
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
