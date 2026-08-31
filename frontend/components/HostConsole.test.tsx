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

  test("supports two to four outcomes, bounded by Add outcome / Remove", async () => {
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

    const addOutcome = screen.getByRole("button", { name: "Add outcome" });
    await user.click(addOutcome);
    await user.click(addOutcome);

    expect(screen.getAllByLabelText(/Outcome/)).toHaveLength(4);
    expect(addOutcome).toBeDisabled();

    const removeOutcome = screen.getByRole("button", { name: "Remove" });
    await user.click(removeOutcome);
    await user.click(removeOutcome);

    expect(screen.getAllByLabelText(/Outcome/)).toHaveLength(2);
    expect(removeOutcome).toBeDisabled();

    await user.click(addOutcome);
    await user.click(addOutcome);

    const outcomeInputs = screen.getAllByLabelText(/Outcome/);
    await user.type(screen.getByLabelText("Question"), "Q?");
    await user.type(outcomeInputs[0], "A");
    await user.type(outcomeInputs[1], "B");
    await user.type(outcomeInputs[2], "C");
    await user.type(outcomeInputs[3], "D");
    await user.click(screen.getByRole("button", { name: "Open round" }));

    expect(onOpenRound).toHaveBeenCalledWith("Q?", ["A", "B", "C", "D"], 30000);
  });

  test("rejects a lock outside 3-120 seconds and accepts the boundaries", async () => {
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

    const fillForm = async () => {
      const outcomeInputs = screen.getAllByLabelText(/Outcome/);
      await user.clear(screen.getByLabelText("Question"));
      await user.type(screen.getByLabelText("Question"), "Q?");
      for (const input of outcomeInputs) {
        await user.clear(input);
      }
      await user.type(outcomeInputs[0], "A");
      await user.type(outcomeInputs[1], "B");
    };

    const lockField = screen.getByLabelText(/Lock/);

    await fillForm();
    await user.clear(lockField);
    await user.type(lockField, "2");
    await user.click(screen.getByRole("button", { name: "Open round" }));
    expect(onOpenRound).not.toHaveBeenCalled();
    expect(screen.getByText("Lock must be between 3 and 120 seconds")).toBeInTheDocument();

    await fillForm();
    await user.clear(lockField);
    await user.type(lockField, "121");
    await user.click(screen.getByRole("button", { name: "Open round" }));
    expect(onOpenRound).not.toHaveBeenCalled();
    expect(screen.getByText("Lock must be between 3 and 120 seconds")).toBeInTheDocument();

    await fillForm();
    await user.clear(lockField);
    await user.type(lockField, "3");
    await user.click(screen.getByRole("button", { name: "Open round" }));
    expect(onOpenRound).toHaveBeenCalledWith("Q?", ["A", "B"], 3000);

    onOpenRound.mockClear();

    await fillForm();
    await user.clear(lockField);
    await user.type(lockField, "120");
    await user.click(screen.getByRole("button", { name: "Open round" }));
    expect(onOpenRound).toHaveBeenCalledWith("Q?", ["A", "B"], 120000);
  });
});
