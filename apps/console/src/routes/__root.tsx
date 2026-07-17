import { ThemeProvider } from "@qeetrix/ui";
import { TanStackDevtools } from "@tanstack/react-devtools";
import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, HeadContent, Scripts } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import type { ReactNode } from "react";
import { Toaster } from "sonner";
import { NotFound, RouteError } from "../components/error-state";
import { APP_TITLE } from "../env";
import TanStackQueryDevtools from "../integrations/tanstack-query/devtools";
import appCss from "../styles.css?url";

const THEME_STORAGE_KEY = "qeet-logs-theme";

// Synchronous head script: runs while the browser parses <head>, before <body>
// renders. Reads the saved theme (or the system preference) and writes the
// matching class onto <html> so the very first paint is correct — without it,
// ThemeProvider only applies the class after hydration, causing a light→dark
// flash on refresh.
const themeFlashScript = `(function(){try{var k="${THEME_STORAGE_KEY}";var t=localStorage.getItem(k);if(t!=="dark"&&t!=="light"&&t!=="system")t="system";var resolved=t==="system"?(window.matchMedia&&window.matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light"):t;var h=document.documentElement;h.classList.remove("light","dark");h.classList.add(resolved);h.style.colorScheme=resolved;}catch(e){}})();`;

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: APP_TITLE },
    ],
    links: [{ rel: "stylesheet", href: appCss }],
  }),
  errorComponent: ({ error, reset }) => <RouteError error={error} reset={reset} />,
  notFoundComponent: () => <NotFound />,
  shellComponent: RootDocument,
});

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        {/* biome-ignore lint/security/noDangerouslySetInnerHtml: inline theme-flash guard */}
        <script dangerouslySetInnerHTML={{ __html: themeFlashScript }} />
        <HeadContent />
      </head>
      <body>
        <ThemeProvider defaultTheme="system" storageKey={THEME_STORAGE_KEY}>
          {children}
          <Toaster position="bottom-right" closeButton richColors />
          <TanStackDevtools
            config={{ position: "bottom-right" }}
            plugins={[
              {
                name: "Tanstack Router",
                render: <TanStackRouterDevtoolsPanel />,
              },
              TanStackQueryDevtools,
            ]}
          />
        </ThemeProvider>
        <Scripts />
      </body>
    </html>
  );
}
