import { describe, expect, it, vi } from "vitest";
import { playCue } from "./audio";

type OscillatorStub = {
  frequency: { value: number };
  connect: ReturnType<typeof vi.fn>;
  start: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
};

type GainStub = {
  gain: { value: number };
  connect: ReturnType<typeof vi.fn>;
};

function makeFakeContext(): {
  context: unknown;
  oscillator: OscillatorStub;
  gain: GainStub;
  createOscillator: ReturnType<typeof vi.fn>;
} {
  const oscillator: OscillatorStub = {
    frequency: { value: 0 },
    connect: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
  };
  const gain: GainStub = {
    gain: { value: 0 },
    connect: vi.fn(),
  };
  const createOscillator = vi.fn(() => oscillator);
  const context = {
    createOscillator,
    createGain: vi.fn(() => gain),
    destination: {},
  };
  return { context, oscillator, gain, createOscillator };
}

describe("playCue", () => {
  it("creates an oscillator, sets a frequency, and starts then stops it", () => {
    const { context, oscillator, createOscillator } = makeFakeContext();

    playCue("open", () => context as never);

    expect(createOscillator).toHaveBeenCalledOnce();
    expect(oscillator.frequency.value).toBeGreaterThan(0);
    expect(oscillator.start).toHaveBeenCalledOnce();
    expect(oscillator.stop).toHaveBeenCalledOnce();
  });

  it("plays a distinct frequency for each cue", () => {
    const openFake = makeFakeContext();
    const lockFake = makeFakeContext();
    const resolveFake = makeFakeContext();

    playCue("open", () => openFake.context as never);
    playCue("lock", () => lockFake.context as never);
    playCue("resolve", () => resolveFake.context as never);

    const frequencies = [
      openFake.oscillator.frequency.value,
      lockFake.oscillator.frequency.value,
      resolveFake.oscillator.frequency.value,
    ];
    expect(new Set(frequencies).size).toBe(3);
  });
});
