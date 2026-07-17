import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SortHeader } from "./sort-header";

function renderHeader(props: Parameters<typeof SortHeader>[0]) {
  return render(
    <table>
      <thead>
        <tr>
          <SortHeader {...props} />
        </tr>
      </thead>
    </table>,
  );
}

describe("SortHeader", () => {
  it("renders its label and reports aria-sort=none when inactive", () => {
    renderHeader({ columnKey: "name", sort: null, onToggle: vi.fn(), children: "Name" });
    const header = screen.getByRole("columnheader");
    expect(header.getAttribute("aria-sort")).toBe("none");
    expect(screen.getByRole("button", { name: /name/i })).toBeTruthy();
  });

  it("reflects the active ascending sort via aria-sort", () => {
    renderHeader({
      columnKey: "name",
      sort: { key: "name", dir: "asc" },
      onToggle: vi.fn(),
      children: "Name",
    });
    expect(screen.getByRole("columnheader").getAttribute("aria-sort")).toBe("ascending");
  });

  it("reflects the active descending sort via aria-sort", () => {
    renderHeader({
      columnKey: "name",
      sort: { key: "name", dir: "desc" },
      onToggle: vi.fn(),
      children: "Name",
    });
    expect(screen.getByRole("columnheader").getAttribute("aria-sort")).toBe("descending");
  });

  it("calls onToggle with the column key when clicked", () => {
    const onToggle = vi.fn();
    renderHeader({ columnKey: "created", sort: null, onToggle, children: "Created" });
    fireEvent.click(screen.getByRole("button", { name: /created/i }));
    expect(onToggle).toHaveBeenCalledWith("created");
  });
});
