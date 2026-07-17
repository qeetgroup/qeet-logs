import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// PageHeader reads the current pathname to auto-resolve its title; stub it.
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: "/incidents" }),
}));

// Ensure the i18n instance is initialized so t() returns real strings.
import "@/i18n";
import { PageHeader } from "./page-header";

describe("PageHeader", () => {
  it("renders an explicit title and description", () => {
    render(<PageHeader title="Custom Title" description="Some description" />);
    expect(screen.getByRole("heading", { name: "Custom Title" })).toBeTruthy();
    expect(screen.getByText("Some description")).toBeTruthy();
  });

  it("auto-resolves the title from the nav config via i18n", () => {
    render(<PageHeader />);
    // /incidents → nav.items.incidents = "Incidents", group = "Investigate".
    expect(screen.getByRole("heading", { name: "Incidents" })).toBeTruthy();
    expect(screen.getByText("Investigate")).toBeTruthy();
  });

  it("renders action content", () => {
    render(<PageHeader title="T" actions={<button type="button">Do it</button>} />);
    expect(screen.getByRole("button", { name: "Do it" })).toBeTruthy();
  });
});
