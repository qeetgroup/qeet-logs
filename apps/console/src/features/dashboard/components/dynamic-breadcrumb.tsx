import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@qeetrix/ui";
import { Link, useLocation } from "@tanstack/react-router";

import { lookupNavTitle } from "@/config/navigation";

export function DynamicBreadcrumb() {
  const { pathname } = useLocation();
  const meta = lookupNavTitle(pathname);

  // Show at most 2 levels: prefer parent for sub-items, else group for top-level.
  const lead = meta.parent
    ? { title: meta.parent.title, url: meta.parent.url }
    : meta.group
      ? { title: meta.group }
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
          <BreadcrumbPage>{meta.title}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}
