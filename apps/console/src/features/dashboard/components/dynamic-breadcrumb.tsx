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
  const { group, parent, title } = lookupNavTitle(pathname);

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {group && (
          <>
            <BreadcrumbItem className="hidden md:block">
              <span className="text-muted-foreground text-sm">{group}</span>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="hidden md:block" />
          </>
        )}
        {parent && (
          <>
            <BreadcrumbItem className="hidden md:block">
              <BreadcrumbLink render={<Link to={parent.url as never} />}>
                {parent.title}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="hidden md:block" />
          </>
        )}
        <BreadcrumbItem>
          <BreadcrumbPage>{title}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}
