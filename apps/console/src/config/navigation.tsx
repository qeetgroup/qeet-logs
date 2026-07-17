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

// Nav labels are stored as i18n keys (`titleKey` / `labelKey`) and resolved
// with `t()` at render time by the sidebar, breadcrumb and page header — so a
// single translation source drives every place a title appears.
export type NavItem = {
  titleKey: string;
  url: string;
  icon?: ReactNode;
  isActive?: boolean;
  items?: { titleKey: string; url: string }[];
};

export type NavGroup = {
  labelKey: string;
  items: NavItem[];
};

export const navGroups: NavGroup[] = [
  {
    labelKey: "nav.groups.monitor",
    items: [
      { titleKey: "nav.items.overview", url: "/", icon: <LayoutDashboardIcon />, isActive: true },
      { titleKey: "nav.items.tail", url: "/tail", icon: <RadioIcon /> },
      { titleKey: "nav.items.topology", url: "/topology", icon: <NetworkIcon /> },
      { titleKey: "nav.items.timeline", url: "/timeline", icon: <HistoryIcon /> },
      { titleKey: "nav.items.dashboards", url: "/dashboards", icon: <LayoutPanelLeftIcon /> },
    ],
  },
  {
    labelKey: "nav.groups.investigate",
    items: [
      { titleKey: "nav.items.search", url: "/search", icon: <ListFilterIcon /> },
      { titleKey: "nav.items.incidents", url: "/incidents", icon: <FlameIcon /> },
      { titleKey: "nav.items.changes", url: "/changes", icon: <GitBranchIcon /> },
      { titleKey: "nav.items.analytics", url: "/analytics", icon: <ChartColumnIcon /> },
      { titleKey: "nav.items.savedSearches", url: "/saved-searches", icon: <BookmarkIcon /> },
      { titleKey: "nav.items.audit", url: "/audit", icon: <ScrollTextIcon /> },
    ],
  },
  {
    labelKey: "nav.groups.respond",
    items: [
      { titleKey: "nav.items.alerts", url: "/alerts", icon: <BellRingIcon /> },
      { titleKey: "nav.items.postmortems", url: "/postmortems", icon: <FileTextIcon /> },
      { titleKey: "nav.items.businessContext", url: "/business-context", icon: <Building2Icon /> },
    ],
  },
  {
    labelKey: "nav.groups.configure",
    items: [
      { titleKey: "nav.items.apiKeys", url: "/api-keys", icon: <KeyRoundIcon /> },
      { titleKey: "nav.items.webhooks", url: "/webhooks", icon: <WebhookIcon /> },
      { titleKey: "nav.items.export", url: "/export", icon: <DownloadIcon /> },
      { titleKey: "nav.items.settings", url: "/settings", icon: <Settings2Icon /> },
    ],
  },
];

export type NavTitleLookup = {
  groupKey?: string;
  parent?: { titleKey: string; url: string };
  /** Resolved i18n key for known nav entries. */
  titleKey?: string;
  /** Literal fallback title (Title-cased slug) for paths not in the nav tree. */
  title?: string;
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
        return { groupKey: group.labelKey, titleKey: item.titleKey };
      }
      const sub = item.items?.find((s) => s.url === pathname);
      if (sub) {
        return {
          groupKey: group.labelKey,
          parent: { titleKey: item.titleKey, url: item.url },
          titleKey: sub.titleKey,
        };
      }
    }
  }
  const segments = pathname.split("/").filter(Boolean);
  return { title: titleFromSlug(segments[segments.length - 1] ?? "Page") };
}
