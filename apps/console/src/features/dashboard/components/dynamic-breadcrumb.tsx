import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@qeetrix/ui";
import { Link, useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { lookupNavTitle } from "@/config/navigation";

export function DynamicBreadcrumb() {
  const { pathname } = useLocation();
  const { t } = useTranslation();
  const meta = lookupNavTitle(pathname);
  const title = meta.titleKey ? t(meta.titleKey) : (meta.title ?? "");

  // Show at most 2 levels: prefer parent for sub-items, else group for top-level.
  const lead = meta.parent
    ? { title: t(meta.parent.titleKey), url: meta.parent.url }
    : meta.groupKey
      ? { title: t(meta.groupKey) }
      : null;

  return (
    <Breadcrumb className="hidden lg:block">
      <BreadcrumbList>
        {lead && (
          <>
            <BreadcrumbItem>
              {"url" in lead && lead.url ? (
                <BreadcrumbLink render={<Link to={lead.url as never} />}>
                  {lead.title}
                </BreadcrumbLink>
              ) : (
                <span className="text-muted-foreground">{lead.title}</span>
              )}
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </>
        )}
        <BreadcrumbItem>
          <BreadcrumbPage>{title}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}
