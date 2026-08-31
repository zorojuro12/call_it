"use client";

import { useState } from "react";
import type { JSX } from "react";

export type WagerPadProps = {
  outcomes: string[];
  balance: number;
  disabled: boolean;
  onPlace: (outcome: number, amount: number) => void;
};

export function WagerPad({ outcomes, balance, disabled, onPlace }: WagerPadProps): JSX.Element {
  const [selected, setSelected] = useState<number | null>(null);
  const [amountText, setAmountText] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleSelect = (index: number) => {
    if (disabled) return;
    setSelected(index);
    setError(null);
  };

  const handleSubmit = () => {
    if (disabled || selected === null) return;

    const amount = Number(amountText);

    if (amount <= 0) {
      setError("Enter an amount above zero");
      return;
    }

    if (amount > balance) {
      setError(`You only have ${balance} tokens`);
      return;
    }

    setError(null);
    onPlace(selected, amount);
  };

  const canSubmit = !disabled && selected !== null;

  return (
    <div>
      {disabled && <p>Betting is closed</p>}
      <div role="group" aria-label="Outcomes">
        {outcomes.map((outcome, index) => (
          <button
            key={outcome}
            type="button"
            aria-pressed={selected === index}
            disabled={disabled}
            onClick={() => handleSelect(index)}
          >
            {outcome}
          </button>
        ))}
      </div>
      <label htmlFor="wager-amount">Amount</label>
      <input
        id="wager-amount"
        type="number"
        disabled={disabled}
        value={amountText}
        onChange={(e) => setAmountText(e.target.value)}
      />
      <button type="button" disabled={!canSubmit} onClick={handleSubmit}>
        Place bet
      </button>
      {error && <p>{error}</p>}
    </div>
  );
}
