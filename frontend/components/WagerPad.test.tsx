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
});
