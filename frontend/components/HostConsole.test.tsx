import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, test, vi } from "vitest";
import { HostConsole } from "./HostConsole";

describe("HostConsole", () => {
  test("opening a round calls onOpenRound with question, outcomes, and lock-in ms", async () => {
    const user = userEvent.setup();
    const onOpenRound = vi.fn();
    const onResolve = vi.fn();

    render(
      <HostConsole
        phase="idle"
        outcomes={[]}
        onOpenRound={onOpenRound}
        onResolve={onResolve}
      />,
    );

    const outcomeInputs = screen.getAllByLabelText(/Outcome/);
    expect(outcomeInputs).toHaveLength(2);

    await user.type(screen.getByLabelText("Question"), "Next goal?");
    await user.type(outcomeInputs[0], "Home");
    await user.type(outcomeInputs[1], "Away");
    await user.clear(screen.getByLabelText(/Lock/));
    await user.type(screen.getByLabelText(/Lock/), "30");
    await user.click(screen.getByRole("button", { name: "Open round" }));

    expect(onOpenRound).toHaveBeenCalledTimes(1);
    expect(onOpenRound).toHaveBeenCalledWith("Next goal?", ["Home", "Away"], 30000);
  });

  test("submitting with an empty question does not call onOpenRound", async () => {
    const user = userEvent.setup();
    const onOpenRound = vi.fn();
    const onResolve = vi.fn();

    render(
      <HostConsole
        phase="idle"
        outcomes={[]}
        onOpenRound={onOpenRound}
        onResolve={onResolve}
      />,
    );

    const outcomeInputs = screen.getAllByLabelText(/Outcome/);
    await user.type(outcomeInputs[0], "Home");
    await user.type(outcomeInputs[1], "Away");
    await user.click(screen.getByRole("button", { name: "Open round" }));

    expect(onOpenRound).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a question")).toBeInTheDocument();
  });
});
