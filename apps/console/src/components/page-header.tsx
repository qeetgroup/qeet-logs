import { useLocation } from "@tanstack/react-router";
import type * as React from "react";
import { useTranslation } from "react-i18next";

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
 * (via i18n keys) based on the current pathname — override with the `title`
 * prop for detail screens whose path isn't in the static nav tree.
 */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  const { pathname } = useLocation();
  const { t } = useTranslation();
  const meta = lookupNavTitle(pathname);
  const resolvedTitle = meta.titleKey ? t(meta.titleKey) : (meta.title ?? "");

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex flex-col gap-1">
        {(meta.groupKey || meta.parent) && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {meta.groupKey && <span>{t(meta.groupKey)}</span>}
            {meta.parent && (
              <>
                <span>›</span>
                <span>{t(meta.parent.titleKey)}</span>
              </>
            )}
          </div>
        )}
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          {title ?? resolvedTitle}
        </h1>
        {description && <p className="max-w-2xl text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
