import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, apiGet, apiPost } from "./api";
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

  it("rejects a 401 error envelope with a typed ApiError", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({
        error: {
          code: "invalid_credentials",
          message: "invalid email or password",
        },
      }),
    });

    // Act & Assert
    await expect(
      apiPost("/api/v1/auth/login", { email: "a", password: "b" }),
    ).rejects.toMatchObject({
      code: "invalid_credentials",
      status: 401,
      message: "invalid email or password",
    });
  });

  it("rejects a 429 error envelope with a typed ApiError", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: false,
      status: 429,
      json: async () => ({
        error: { code: "rate_limited", message: "too many requests" },
      }),
    });

    // Act & Assert
    await expect(apiPost("/api/v1/rooms", {})).rejects.toMatchObject({
      code: "rate_limited",
      status: 429,
    });
  });

  it("rejects a non-JSON error body as internal_error", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => {
        throw new SyntaxError("Unexpected token B in JSON");
      },
    });

    // Act
    let caught: unknown;
    try {
      await apiPost("/api/v1/rooms", {});
    } catch (err) {
      caught = err;
    }

    // Assert
    expect(caught).toBeInstanceOf(ApiError);
    expect((caught as ApiError).status).toBe(500);
    expect((caught as ApiError).code).toBe("internal_error");
  });

  it("rejects a network failure as network_error", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValue(
      new TypeError("Failed to fetch"),
    );

    // Act
    let caught: unknown;
    try {
      await apiPost("/api/v1/rooms", {});
    } catch (err) {
      caught = err;
    }

    // Assert
    expect(caught).toBeInstanceOf(ApiError);
    expect((caught as ApiError).code).toBe("network_error");
    expect((caught as ApiError).status).toBe(0);
  });

  it("attaches a bearer token to apiPost when supplied", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: { ok: true } }),
    });

    // Act
    await apiPost("/api/v1/rooms", { buy_in: 1000 }, "tok123");

    // Assert
    const init = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>)["Authorization"]).toBe(
      "Bearer tok123",
    );
  });

  it("sends no Authorization header for apiPost when no token is supplied", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: { ok: true } }),
    });

    // Act
    await apiPost("/api/v1/rooms", { buy_in: 1000 });

    // Assert
    const init = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    expect(
      Object.prototype.hasOwnProperty.call(init.headers, "Authorization"),
    ).toBe(false);
  });

  it("attaches a bearer token to apiGet with no request body", async () => {
    // Arrange
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: { ok: true } }),
    });

    // Act
    await apiGet("/api/v1/whatever", "tok123");

    // Assert
    const call = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    const init = call[1] as RequestInit;
    expect(init.method).toBe("GET");
    expect((init.headers as Record<string, string>)["Authorization"]).toBe(
      "Bearer tok123",
    );
    expect(init.body).toBeUndefined();
  });
});
