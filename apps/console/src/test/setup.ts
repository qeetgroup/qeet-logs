// Vitest setup: runs before every test file.
//
// jsdom omits a few browser APIs that @qeetrix/ui (Base UI) and the theme layer
// rely on, so we polyfill the minimum surface here. We also register React
// Testing Library's auto-cleanup so mounted trees don't leak between tests.
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(() => {
  cleanup();
});

// matchMedia — used by the theme provider and responsive hooks.
if (!window.matchMedia) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

// Minimal observer stub covering the ResizeObserver / IntersectionObserver
// surface Base UI touches (ScrollArea, virtualized rows, charts).
class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

if (!("ResizeObserver" in globalThis)) {
  globalThis.ResizeObserver = ObserverStub as unknown as typeof ResizeObserver;
}

if (!("IntersectionObserver" in globalThis)) {
  globalThis.IntersectionObserver = ObserverStub as unknown as typeof IntersectionObserver;
}
