"use client";

import { useState } from "react";
import type { JSX } from "react";
import type { Phase } from "../lib/roundState";

export type HostConsoleProps = {
  phase: Phase;
  outcomes: string[]; // the open round's outcomes, for the resolve picker
  onOpenRound: (question: string, outcomes: string[], lockInMs: number) => void;
  onResolve: (winningOutcome: number) => void;
};

export function HostConsole({ outcomes, onOpenRound }: HostConsoleProps): JSX.Element {
  const [question, setQuestion] = useState("");
  const [outcomeFields, setOutcomeFields] = useState<string[]>(["", ""]);
  const [lockSeconds, setLockSeconds] = useState("30");
  const [error, setError] = useState<string | null>(null);

  const handleOutcomeChange = (index: number, value: string) => {
    setOutcomeFields((fields) => fields.map((f, i) => (i === index ? value : f)));
  };

  const handleSubmit = () => {
    const trimmedQuestion = question.trim();
    const trimmedOutcomes = outcomeFields.map((o) => o.trim());

    if (!trimmedQuestion) {
      setError("Enter a question");
      return;
    }

    if (trimmedOutcomes.some((o) => !o)) {
      setError("Every outcome needs a label");
      return;
    }

    const seconds = Number(lockSeconds);
    setError(null);
    onOpenRound(trimmedQuestion, trimmedOutcomes, seconds * 1000);
  };

  return (
    <div>
      <label htmlFor="host-question">Question</label>
      <input
        id="host-question"
        type="text"
        value={question}
        onChange={(e) => setQuestion(e.target.value)}
      />

      {outcomeFields.map((value, index) => (
        <div key={index}>
          <label htmlFor={`host-outcome-${index}`}>Outcome {index + 1}</label>
          <input
            id={`host-outcome-${index}`}
            type="text"
            value={value}
            onChange={(e) => handleOutcomeChange(index, e.target.value)}
          />
        </div>
      ))}

      <label htmlFor="host-lock-seconds">Lock (seconds)</label>
      <input
        id="host-lock-seconds"
        type="number"
        value={lockSeconds}
        onChange={(e) => setLockSeconds(e.target.value)}
      />

      <button type="button" onClick={handleSubmit}>
        Open round
      </button>

      {error && <p>{error}</p>}
    </div>
  );
}
