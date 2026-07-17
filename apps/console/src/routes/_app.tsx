import { Button, Separator, SidebarInset, SidebarProvider, SidebarTrigger } from "@qeetrix/ui";
import {
  CatchBoundary,
  createFileRoute,
  Outlet,
  useLocation,
  useNavigate,
} from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import { ContentError, RouteError } from "@/components/error-state";
import { AppSidebar } from "@/features/dashboard/components/app-sidebar";
import { DynamicBreadcrumb } from "@/features/dashboard/components/dynamic-breadcrumb";
import { HeaderUser } from "@/features/dashboard/components/header-user";
import { ThemeToggle } from "@/features/dashboard/components/theme-toggle";
import { isAuthenticated } from "@/lib/auth";

export const Route = createFileRoute("/_app")({
  component: AppLayout,
  errorComponent: ({ error, reset }) => <RouteError error={error} reset={reset} />,
});

// The auth guard runs as a useEffect, not in beforeLoad, because the API key
// lives in localStorage and is therefore invisible to the server. Running it in
// beforeLoad would redirect every hard refresh to /sign-in even with a valid key.
function AppLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useTranslation();

  useEffect(() => {
    if (!isAuthenticated()) {
      navigate({ to: "/sign-in", replace: true });
    }
  }, [navigate]);

  return (
    <SidebarProvider>
      {/* Skip link: first focusable element, visually hidden until focused so
          keyboard users can jump past the sidebar/header to content. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:inset-s-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:shadow-md focus:ring-2 focus:ring-ring focus:outline-none"
      >
        {t("nav.skipToContent")}
      </a>
      <AppSidebar />
      {/* SidebarInset renders the page's <main> landmark. */}
      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 border-b px-3 sm:px-4">
          {/* Left */}
          <div className="flex min-w-0 items-center gap-2">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 hidden h-4 lg:block" />
            <DynamicBreadcrumb />
          </div>

          {/* Center — search-as-button that jumps to the LogQL++ editor */}
          <button
            type="button"
            onClick={() => navigate({ to: "/search" })}
            className="relative mx-auto hidden h-9 w-full max-w-md items-center rounded-lg border bg-background ps-9 pe-3 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/50 md:flex"
            aria-label={t("nav.openSearch")}
          >
            <SearchIcon className="pointer-events-none absolute inset-s-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <span>{t("nav.searchPlaceholder")}</span>
          </button>

          {/* Right */}
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label={t("nav.openSearch")}
              onClick={() => navigate({ to: "/search" })}
            >
              <SearchIcon />
            </Button>
            <ThemeToggle />
            <Separator orientation="vertical" className="mx-1 hidden h-6 sm:block" />
            <HeaderUser />
          </div>
        </header>
        {/* Content region. The <main> landmark is SidebarInset (above), so this
            is a plain focusable container that the skip link targets. */}
        <div
          id="main-content"
          tabIndex={-1}
          className="flex min-w-0 flex-1 flex-col gap-4 p-4 focus:outline-none"
        >
          <CatchBoundary
            getResetKey={() => location.pathname}
            errorComponent={({ error, reset }) => <ContentError error={error} reset={reset} />}
          >
            <Outlet />
          </CatchBoundary>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
