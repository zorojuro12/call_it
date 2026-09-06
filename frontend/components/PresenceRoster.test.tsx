import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { PresenceRoster } from "./PresenceRoster";

describe("PresenceRoster", () => {
  test("renders each player and marks the current user as you", () => {
    render(
      <PresenceRoster
        players={[
          { user_id: "u1", display_name: "Ann" },
          { user_id: "u2", display_name: "Bo" },
        ]}
        selfId="u1"
      />,
    );

    const list = screen.getByRole("list");
    const items = screen.getAllByRole("listitem");
    expect(list).toBeInTheDocument();
    expect(items).toHaveLength(2);

    const annItem = items.find((item) => item.textContent?.includes("Ann"));
    const boItem = items.find((item) => item.textContent?.includes("Bo"));
    expect(annItem?.textContent).toMatch(/you/i);
    expect(boItem?.textContent).not.toMatch(/you/i);
  });
});
