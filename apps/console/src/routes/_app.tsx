import {
  Button,
  Separator,
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@qeetrix/ui";
import { Outlet, createFileRoute, useNavigate } from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useEffect } from "react";

import { AppSidebar } from "@/features/dashboard/components/app-sidebar";
import { DynamicBreadcrumb } from "@/features/dashboard/components/dynamic-breadcrumb";
import { HeaderUser } from "@/features/dashboard/components/header-user";
import { ThemeToggle } from "@/features/dashboard/components/theme-toggle";
import { isAuthenticated } from "@/lib/auth";

export const Route = createFileRoute("/_app")({ component: AppLayout });

function AppLayout() {
  const navigate = useNavigate();

  useEffect(() => {
    if (!isAuthenticated()) {
      navigate({ to: "/sign-in", replace: true });
    }
  }, [navigate]);

  return (
    <SidebarProvider>
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:inset-s-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:shadow-md focus:ring-2 focus:ring-ring focus:outline-none"
      >
        Skip to main content
      </a>

      <AppSidebar />

      <SidebarInset>
        <header className="flex h-16 shrink-0 items-center gap-2 border-b px-3 sm:px-4">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 hidden h-4 lg:block" />
            <DynamicBreadcrumb />
          </div>

          {/* Center: search placeholder (no cmd-K palette yet) */}
          <div className="relative mx-auto hidden h-9 w-full max-w-md items-center rounded-lg border bg-background ps-9 pe-3 text-sm text-muted-foreground md:flex cursor-not-allowed opacity-50">
            <SearchIcon className="pointer-events-none absolute inset-s-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <span>Search logs… (use Log Search)</span>
          </div>

          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="icon" className="md:hidden" aria-label="Search" disabled>
              <SearchIcon />
            </Button>
            <ThemeToggle />
            <Separator orientation="vertical" className="mx-1 hidden h-6 sm:block" />
            <HeaderUser />
          </div>
        </header>

        <main
          id="main-content"
          tabIndex={-1}
          className="flex min-w-0 flex-1 flex-col gap-4 p-4 focus:outline-none"
        >
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
