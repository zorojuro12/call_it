import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { OddsBoard } from "./OddsBoard";

describe("OddsBoard", () => {
  test("shows each outcome's pool and multiplier plus the total", () => {
    render(
      <OddsBoard
        outcomes={["Home", "Away"]}
        pools={[300, 100]}
        total={400}
        multipliers={[1.3333, 4]}
        bettors={2}
        players={5}
      />,
    );

    expect(screen.getByText("Home")).toBeInTheDocument();
    expect(screen.getByText("Away")).toBeInTheDocument();
    expect(screen.getByText("300")).toBeInTheDocument();
    expect(screen.getByText("100")).toBeInTheDocument();
    expect(screen.getByText(/400/)).toBeInTheDocument();
    expect(screen.getByText("1.33×")).toBeInTheDocument();
    expect(screen.getByText("4.00×")).toBeInTheDocument();
  });

  test("shows a dash, not 0.00x, for an outcome with an empty pool", () => {
    render(
      <OddsBoard
        outcomes={["Home", "Away"]}
        pools={[0, 100]}
        total={100}
        multipliers={[0, 1]}
        bettors={1}
        players={5}
      />,
    );

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("0.00×")).not.toBeInTheDocument();
    expect(screen.getByText("1.00×")).toBeInTheDocument();
  });
});
