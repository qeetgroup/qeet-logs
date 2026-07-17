import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ResultsTable } from "./results-table";

const envelope = {
  columns: ["service", "level"],
  count: 1,
  rows: [{ service: "checkout", level: "error" }],
};

describe("ResultsTable", () => {
  it("renders column headers and row cells from the envelope", () => {
    render(<ResultsTable data={envelope} />);
    expect(screen.getByText("service")).toBeTruthy();
    expect(screen.getByText("level")).toBeTruthy();
    expect(screen.getByText("checkout")).toBeTruthy();
    expect(screen.getByText("error")).toBeTruthy();
  });

  it("shows the empty state when there are no rows", () => {
    render(
      <ResultsTable
        data={{ columns: [], count: 0, rows: [] }}
        emptyTitle="Nothing here"
        emptyDescription="Try a different query"
      />,
    );
    expect(screen.getByText("Nothing here")).toBeTruthy();
    expect(screen.getByText("Try a different query")).toBeTruthy();
  });

  it("surfaces an error message when the query failed", () => {
    render(<ResultsTable isError error={new Error("query blew up")} />);
    expect(screen.getByText(/query blew up/)).toBeTruthy();
  });
});
