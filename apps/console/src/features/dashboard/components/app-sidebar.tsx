import { Sidebar, SidebarContent, SidebarFooter, SidebarHeader, SidebarRail } from "@qeetrix/ui";
import type * as React from "react";

import { navGroups } from "@/config/navigation";
import { BrandHeader } from "./brand-header";
import { NavMain } from "./nav-main";
import { NavUser } from "./nav-user";

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <BrandHeader />
      </SidebarHeader>
      <SidebarContent>
        <NavMain groups={navGroups} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
