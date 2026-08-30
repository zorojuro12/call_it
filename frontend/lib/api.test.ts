import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "./api";
import type { CreateRoomResponse } from "./protocol";

describe("apiPost", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("unwraps a success envelope to its data", async () => {
    // Arrange
    const body = {
      data: { room_id: "r1", code: "ABC123", buy_in: 1000, token: "t" },
    };
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => body,
    });

    // Act
    const result = await apiPost<CreateRoomResponse>("/api/v1/rooms", {
      buy_in: 1000,
    });

    // Assert
    expect(result).toEqual({
      room_id: "r1",
      code: "ABC123",
      buy_in: 1000,
      token: "t",
    });

    const call = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe("http://localhost:8080/api/v1/rooms");
    const init = call[1] as RequestInit;
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/json",
    );
    expect(JSON.parse(init.body as string)).toEqual({ buy_in: 1000 });
  });
});
