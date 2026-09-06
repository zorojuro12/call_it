import { describe, expect, test } from "vitest";
import { resolveApiBaseUrl } from "./config";

describe("resolveApiBaseUrl", () => {
  test("defaults to localhost:8080 outside production when unset", () => {
    expect(resolveApiBaseUrl({ NODE_ENV: "development" })).toBe(
      "http://localhost:8080",
    );
  });

  test("returns an explicit value unchanged", () => {
    expect(
      resolveApiBaseUrl({
        NEXT_PUBLIC_API_BASE_URL: "http://api.test:9000",
        NODE_ENV: "development",
      }),
    ).toBe("http://api.test:9000");
  });

  test("strips exactly one trailing slash", () => {
    expect(
      resolveApiBaseUrl({
        NEXT_PUBLIC_API_BASE_URL: "http://api.test:9000/",
        NODE_ENV: "production",
      }),
    ).toBe("http://api.test:9000");
  });

  test("throws in production when unset", () => {
    expect(() => resolveApiBaseUrl({ NODE_ENV: "production" })).toThrow(
      /NEXT_PUBLIC_API_BASE_URL/,
    );
  });

  test("throws in production when empty", () => {
    expect(() =>
      resolveApiBaseUrl({
        NEXT_PUBLIC_API_BASE_URL: "",
        NODE_ENV: "production",
      }),
    ).toThrow(/NEXT_PUBLIC_API_BASE_URL/);
  });
});
