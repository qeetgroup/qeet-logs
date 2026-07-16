import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@qeetrix/ui";
import { Link } from "@tanstack/react-router";
import { ScrollTextIcon } from "lucide-react";

// Sidebar header brand block. Qeet Logs is a single-product console (no team
// switcher), so this is a static brand lockup that also links home.
export function BrandHeader() {
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton size="lg" render={<Link to="/" />}>
          <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <ScrollTextIcon className="size-4" />
          </div>
          <div className="grid flex-1 text-start text-sm leading-tight">
            <span className="truncate font-semibold">Qeet Logs</span>
            <span className="truncate text-xs text-muted-foreground">Log management</span>
          </div>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
