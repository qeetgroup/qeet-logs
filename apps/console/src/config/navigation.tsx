import {
  AlertTriangleIcon,
  BarChart2Icon,
  BookmarkIcon,
  DatabaseIcon,
  GaugeIcon,
  KeyRoundIcon,
  LayoutDashboardIcon,
  LayoutPanelLeftIcon,
  ListFilterIcon,
  RadioIcon,
  ScrollTextIcon,
  Settings2Icon,
} from "lucide-react";
import type { ReactNode } from "react";

export type NavItem = {
  title: string;
  url: string;
  icon?: ReactNode;
  isActive?: boolean;
  items?: { title: string; url: string }[];
};

export type NavGroup = {
  label: string;
  items: NavItem[];
};

export const navGroups: NavGroup[] = [
  {
    label: "Monitor",
    items: [
      { title: "Overview", url: "/", icon: <LayoutDashboardIcon />, isActive: true },
      { title: "Live Tail", url: "/tail", icon: <RadioIcon /> },
      { title: "Dashboards", url: "/dashboards", icon: <LayoutPanelLeftIcon /> },
    ],
  },
  {
    label: "Query",
    items: [
      { title: "Log Search", url: "/search", icon: <ListFilterIcon /> },
      { title: "Saved Searches", url: "/saved-searches", icon: <BookmarkIcon /> },
      { title: "Audit Log", url: "/audit", icon: <ScrollTextIcon /> },
    ],
  },
  {
    label: "Configure",
    items: [
      { title: "Alert Rules", url: "/alerts", icon: <AlertTriangleIcon /> },
      { title: "API Keys", url: "/api-keys", icon: <KeyRoundIcon /> },
      { title: "Settings", url: "/settings", icon: <Settings2Icon /> },
    ],
  },
];

// These exist for breadcrumb resolution but not primary navigation.
const _extra: NavItem[] = [
  { title: "Log Search", url: "/search", icon: <BarChart2Icon /> },
  { title: "Database", url: "/settings", icon: <DatabaseIcon /> },
  { title: "Gauge", url: "/", icon: <GaugeIcon /> },
];
void _extra;

export type NavTitleLookup = {
  group?: string;
  parent?: { title: string; url: string };
  title: string;
};

function titleFromSlug(slug: string): string {
  return slug
    .split("-")
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(" ");
}

export function lookupNavTitle(pathname: string): NavTitleLookup {
  for (const group of navGroups) {
    for (const item of group.items) {
      if (item.url === pathname) return { group: group.label, title: item.title };
      const sub = item.items?.find((s) => s.url === pathname);
      if (sub) return { group: group.label, parent: { title: item.title, url: item.url }, title: sub.title };
    }
  }
  const segments = pathname.split("/").filter(Boolean);
  return { title: titleFromSlug(segments[segments.length - 1] ?? "Page") };
}
