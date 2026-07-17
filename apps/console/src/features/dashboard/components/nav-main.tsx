import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@qeetrix/ui";
import { Link } from "@tanstack/react-router";
import { ChevronRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { NavGroup, NavItem } from "@/config/navigation";

// Mark the active route for assistive tech. TanStack's Link applies these
// props only when its `to` matches the current location.
const activeProps = { "aria-current": "page" as const };

function NavMenuItem({ item }: { item: NavItem }) {
  const { t } = useTranslation();
  const title = t(item.titleKey);

  if (!item.items?.length) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton
          tooltip={title}
          render={<Link to={item.url as never} activeProps={activeProps} />}
        >
          {item.icon}
          <span>{title}</span>
        </SidebarMenuButton>
      </SidebarMenuItem>
    );
  }

  return (
    <Collapsible
      defaultOpen={item.isActive}
      className="group/collapsible"
      render={<SidebarMenuItem />}
    >
      <CollapsibleTrigger render={<SidebarMenuButton tooltip={title} />}>
        {item.icon}
        <span>{title}</span>
        <ChevronRightIcon className="ms-auto transition-transform duration-200 group-data-open/collapsible:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <SidebarMenuSub>
          {item.items.map((subItem) => (
            <SidebarMenuSubItem key={subItem.titleKey}>
              <SidebarMenuSubButton
                render={<Link to={subItem.url as never} activeProps={activeProps} />}
              >
                <span>{t(subItem.titleKey)}</span>
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      </CollapsibleContent>
    </Collapsible>
  );
}

export function NavMain({ groups }: { groups: NavGroup[] }) {
  const { t } = useTranslation();
  return (
    <nav aria-label={t("nav.ariaLabel")}>
      {groups.map((group) => (
        <SidebarGroup key={group.labelKey}>
          <SidebarGroupLabel className="text-[10px] uppercase tracking-widest">
            {t(group.labelKey)}
          </SidebarGroupLabel>
          <SidebarMenu>
            {group.items.map((item) => (
              <NavMenuItem key={item.titleKey} item={item} />
            ))}
          </SidebarMenu>
        </SidebarGroup>
      ))}
    </nav>
  );
}
