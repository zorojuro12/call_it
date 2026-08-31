import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { SettlementReveal } from "./SettlementReveal";

describe("SettlementReveal", () => {
  test("shows the winning outcome and a row per player with staked, returned, and net", () => {
    render(
      <SettlementReveal
        results={[
          { user_id: "u1", display_name: "Ann", staked: 100, returned: 250, net: 150 },
          { user_id: "u2", display_name: "Bob", staked: 100, returned: 0, net: -100 },
        ]}
        outcomes={["Home", "Away"]}
        winningOutcome={0}
        dust={3}
        refunded={false}
        refundTotal={null}
        selfId="u1"
      />,
    );

    expect(screen.getByText("Home")).toBeInTheDocument();

    const annRow = screen.getByText(/Ann.*\(you\)/).closest("tr");
    expect(annRow).not.toBeNull();
    expect(annRow!).toHaveTextContent("100");
    expect(annRow!).toHaveTextContent("250");
    expect(annRow!).toHaveTextContent("+150");

    const bobRow = screen.getByText("Bob").closest("tr");
    expect(bobRow).not.toBeNull();
    expect(bobRow!).toHaveTextContent("100");
    expect(bobRow!).toHaveTextContent("0");
    expect(bobRow!).toHaveTextContent("-100");

    expect(screen.getByText(/Dust: 3/)).toBeInTheDocument();
  });

  test("nobody backed the winning outcome — shows the refund message and still renders per-player rows", () => {
    render(
      <SettlementReveal
        results={[{ user_id: "u1", display_name: "Ann", staked: 100, returned: 100, net: 0 }]}
        outcomes={["Home", "Away"]}
        winningOutcome={1}
        dust={0}
        refunded={true}
        refundTotal={null}
        selfId="u1"
      />,
    );

    expect(
      screen.getByText("Nobody backed the winning outcome — every stake was returned"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Ann/)).toBeInTheDocument();
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  test("host-disconnect fallback — shows the unresolved message and renders no per-player rows", () => {
    render(
      <SettlementReveal
        results={null}
        outcomes={["Home", "Away"]}
        winningOutcome={null}
        dust={0}
        refunded={true}
        refundTotal={400}
        selfId="u1"
      />,
    );

    expect(
      screen.getByText("The round went unresolved — all 400 tokens were refunded"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});
