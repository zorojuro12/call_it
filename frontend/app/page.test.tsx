import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import Page from "./page";

describe("landing page", () => {
  test("renders the app shell", () => {
    render(<Page />);

    expect(
      screen.getByRole("heading", { name: /CallIt/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: /room code/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /join/i }),
    ).toBeInTheDocument();
  });
});
