import {
  BellRingIcon,
  BookmarkIcon,
  Building2Icon,
  ChartColumnIcon,
  DownloadIcon,
  FileTextIcon,
  FlameIcon,
  GitBranchIcon,
  HistoryIcon,
  KeyRoundIcon,
  LayoutDashboardIcon,
  LayoutPanelLeftIcon,
  ListFilterIcon,
  NetworkIcon,
  RadioIcon,
  ScrollTextIcon,
  Settings2Icon,
  WebhookIcon,
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
      { title: "Service Topology", url: "/topology", icon: <NetworkIcon /> },
      { title: "Timeline", url: "/timeline", icon: <HistoryIcon /> },
      { title: "Dashboards", url: "/dashboards", icon: <LayoutPanelLeftIcon /> },
    ],
  },
  {
    label: "Investigate",
    items: [
      { title: "Log Search", url: "/search", icon: <ListFilterIcon /> },
      { title: "Incidents", url: "/incidents", icon: <FlameIcon /> },
      { title: "Changes", url: "/changes", icon: <GitBranchIcon /> },
      { title: "Analytics", url: "/analytics", icon: <ChartColumnIcon /> },
      { title: "Saved Searches", url: "/saved-searches", icon: <BookmarkIcon /> },
      { title: "Audit Log", url: "/audit", icon: <ScrollTextIcon /> },
    ],
  },
  {
    label: "Respond",
    items: [
      { title: "Alert Rules", url: "/alerts", icon: <BellRingIcon /> },
      { title: "Postmortems", url: "/postmortems", icon: <FileTextIcon /> },
      { title: "Business Context", url: "/business-context", icon: <Building2Icon /> },
    ],
  },
  {
    label: "Configure",
    items: [
      { title: "API Keys", url: "/api-keys", icon: <KeyRoundIcon /> },
      { title: "Webhooks", url: "/webhooks", icon: <WebhookIcon /> },
      { title: "Export", url: "/export", icon: <DownloadIcon /> },
      { title: "Settings", url: "/settings", icon: <Settings2Icon /> },
    ],
  },
];

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
      if (item.url === pathname) {
        return { group: group.label, title: item.title };
      }
      const sub = item.items?.find((s) => s.url === pathname);
      if (sub) {
        return {
          group: group.label,
          parent: { title: item.title, url: item.url },
          title: sub.title,
        };
      }
    }
  }
  const segments = pathname.split("/").filter(Boolean);
  return { title: titleFromSlug(segments[segments.length - 1] ?? "Page") };
}
