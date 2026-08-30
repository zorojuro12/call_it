const DEFAULT_DEV_API_BASE_URL = "http://localhost:8080";

export function resolveApiBaseUrl(
  env: Record<string, string | undefined>,
): string {
  const raw = env.NEXT_PUBLIC_API_BASE_URL?.trim();

  if (!raw) {
    if (env.NODE_ENV === "production") {
      throw new Error(
        "NEXT_PUBLIC_API_BASE_URL is required when NODE_ENV=production",
      );
    }
    return DEFAULT_DEV_API_BASE_URL;
  }

  return raw.endsWith("/") ? raw.slice(0, -1) : raw;
}

export const API_BASE_URL = resolveApiBaseUrl(process.env);
