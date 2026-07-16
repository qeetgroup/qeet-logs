import { useLocation } from "@tanstack/react-router";
import type * as React from "react";

import { lookupNavTitle } from "@/config/navigation";

type PageHeaderProps = {
  /** Overrides the auto-detected title (useful for detail pages). */
  title?: string;
  /** One-line description shown below the title. */
  description?: string;
  /** Optional action area (buttons, dropdowns) shown on the right side. */
  actions?: React.ReactNode;
};

/**
 * Standard top-of-page header. Title auto-resolves from the navigation config
 * based on the current pathname — override with the `title` prop for detail
 * screens whose path isn't in the static nav tree.
 */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  const { pathname } = useLocation();
  const meta = lookupNavTitle(pathname);

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex flex-col gap-1">
        {(meta.group || meta.parent) && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {meta.group && <span>{meta.group}</span>}
            {meta.parent && (
              <>
                <span>›</span>
                <span>{meta.parent.title}</span>
              </>
            )}
          </div>
        )}
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          {title ?? meta.title}
        </h1>
        {description && <p className="max-w-2xl text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
