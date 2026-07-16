import { Button, Card, CardContent, cn } from "@qeetrix/ui";
import { Link, useRouter } from "@tanstack/react-router";
import { HouseIcon, RotateCwIcon, SearchXIcon, TriangleAlertIcon } from "lucide-react";
import type { ComponentType, ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@/lib/api";

/** Pull a human-readable message out of any thrown value. */
export function errorMessage(error: unknown): string | undefined {
  if (!error) return undefined;
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return undefined;
}

type ErrorStateProps = {
  /** Heading. Defaults to a translated generic error title. */
  title?: ReactNode;
  /** Body copy. Falls back to the error's message, then a generic line. */
  description?: ReactNode;
  /** Raw error — its `.message` is shown when `description` is omitted. */
  error?: unknown;
  /** Retry handler; renders a "Try again" button when provided. */
  onRetry?: () => void;
  retryLabel?: string;
  icon?: ComponentType<{ className?: string }>;
  /** Extra actions (e.g. a link home) rendered beside Retry. */
  action?: ReactNode;
  className?: string;
};

/**
 * Branded, centred error panel. Pairs with DataState (`errorFallback={<ErrorState … />}`)
 * and backs the router error boundaries. Keeps the same visual language as
 * EmptyState so a failed surface never collapses to a blank screen.
 */
export function ErrorState({
  title,
  description,
  error,
  onRetry,
  retryLabel,
  icon: Icon = TriangleAlertIcon,
  action,
  className,
}: ErrorStateProps) {
  const { t } = useTranslation();
  const body = description ?? errorMessage(error) ?? t("states.errorDescription");

  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-center justify-center gap-3 px-6 py-12 text-center",
        className,
      )}
    >
      <span className="flex size-11 items-center justify-center rounded-full bg-destructive/10 text-destructive">
        <Icon className="size-5" />
      </span>
      <div className="flex flex-col gap-1">
        <h2 className="font-heading text-base font-semibold">{title ?? t("states.errorTitle")}</h2>
        <p className="max-w-md text-sm text-muted-foreground">{body}</p>
      </div>
      {(onRetry || action) && (
        <div className="mt-1 flex items-center gap-2">
          {onRetry && (
            <Button variant="outline" size="sm" onClick={onRetry}>
              <RotateCwIcon />
              {retryLabel ?? t("states.retry")}
            </Button>
          )}
          {action}
        </div>
      )}
    </div>
  );
}

type RouteErrorProps = {
  error: unknown;
  reset?: () => void;
};

/**
 * Full-page error boundary used as the `errorComponent` on `__root` and `_app`.
 * Retrying invalidates the router (re-runs loaders) and resets the boundary.
 */
export function RouteError({ error, reset }: RouteErrorProps) {
  const { t } = useTranslation();
  const router = useRouter();

  function retry() {
    void router.invalidate();
    reset?.();
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 p-6">
      <Card className="w-full max-w-md">
        <CardContent className="pt-6">
          <ErrorState
            title={t("errors.route.title")}
            description={errorMessage(error) ?? t("errors.route.description")}
            onRetry={retry}
            action={
              <Button variant="ghost" size="sm" render={<Link to="/" />}>
                <HouseIcon />
                {t("errors.home")}
              </Button>
            }
          />
        </CardContent>
      </Card>
    </div>
  );
}

/**
 * In-layout error boundary for the content area (keeps the sidebar/header).
 * Used via CatchBoundary around the router Outlet.
 */
export function ContentError({ error, reset }: RouteErrorProps) {
  const { t } = useTranslation();
  const router = useRouter();

  function retry() {
    void router.invalidate();
    reset?.();
  }

  return (
    <Card>
      <CardContent className="pt-6">
        <ErrorState
          title={t("errors.content.title")}
          description={errorMessage(error) ?? t("errors.content.description")}
          onRetry={retry}
        />
      </CardContent>
    </Card>
  );
}

/** `notFoundComponent` for unmatched routes. */
export function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 p-6">
      <Card className="w-full max-w-md">
        <CardContent className="pt-6">
          <ErrorState
            icon={SearchXIcon}
            title={t("errors.notFound.title")}
            description={t("errors.notFound.description")}
            action={
              <Button size="sm" render={<Link to="/" />}>
                <HouseIcon />
                {t("errors.notFound.home")}
              </Button>
            }
          />
        </CardContent>
      </Card>
    </div>
  );
}
