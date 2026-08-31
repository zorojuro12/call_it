import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test, vi } from "vitest";
import { WagerPad } from "./WagerPad";

describe("WagerPad", () => {
  test("selecting an outcome and staking calls onPlace with the outcome index and amount", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad
        outcomes={["Home", "Away"]}
        balance={1000}
        disabled={false}
        onPlace={onPlace}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Away" }));
    await user.type(screen.getByLabelText("Amount"), "150");
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    expect(onPlace).toHaveBeenCalledTimes(1);
    expect(onPlace).toHaveBeenCalledWith(1, 150);
  });

  test("with no outcome selected, Place bet is disabled and does not call onPlace", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad
        outcomes={["Home", "Away"]}
        balance={1000}
        disabled={false}
        onPlace={onPlace}
      />,
    );

    const placeBet = screen.getByRole("button", { name: "Place bet" });
    expect(placeBet).toBeDisabled();

    await user.click(placeBet);

    expect(onPlace).not.toHaveBeenCalled();
  });

  test("refuses a stake above the balance and shows the limit", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad outcomes={["Home", "Away"]} balance={100} disabled={false} onPlace={onPlace} />,
    );

    await user.click(screen.getByRole("button", { name: "Home" }));
    await user.type(screen.getByLabelText("Amount"), "150");
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    expect(onPlace).not.toHaveBeenCalled();
    expect(screen.getByText("You only have 100 tokens")).toBeInTheDocument();
  });

  test("accepts a stake exactly equal to the balance", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad outcomes={["Home", "Away"]} balance={100} disabled={false} onPlace={onPlace} />,
    );

    await user.click(screen.getByRole("button", { name: "Home" }));
    await user.type(screen.getByLabelText("Amount"), "100");
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    expect(onPlace).toHaveBeenCalledWith(0, 100);
  });

  test("rejects a zero amount", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad outcomes={["Home", "Away"]} balance={100} disabled={false} onPlace={onPlace} />,
    );

    await user.click(screen.getByRole("button", { name: "Home" }));
    await user.type(screen.getByLabelText("Amount"), "0");
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    expect(onPlace).not.toHaveBeenCalled();
    expect(screen.getByText("Enter an amount above zero")).toBeInTheDocument();
  });

  test("a locked round disables every control", async () => {
    const user = userEvent.setup();
    const onPlace = vi.fn();

    render(
      <WagerPad outcomes={["Home", "Away"]} balance={1000} disabled={true} onPlace={onPlace} />,
    );

    expect(screen.getByRole("button", { name: "Home" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Away" })).toBeDisabled();
    expect(screen.getByLabelText("Amount")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Place bet" })).toBeDisabled();
    expect(screen.getByText("Betting is closed")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Home" }));
    await user.click(screen.getByRole("button", { name: "Place bet" }));

    expect(onPlace).not.toHaveBeenCalled();
  });
});
