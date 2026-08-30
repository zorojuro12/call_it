import { API_BASE_URL } from "./config";

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(message: string, code: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

function buildHeaders(
  hasBody: boolean,
  token?: string,
): Record<string, string> {
  const headers: Record<string, string> = {};

  if (hasBody) {
    headers["Content-Type"] = "application/json";
  }

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  return headers;
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (response.ok) {
    const body = await response.json();
    return body.data as T;
  }

  let parsed: { error?: { code: string; message: string } } | undefined;
  try {
    parsed = await response.json();
  } catch {
    parsed = undefined;
  }

  if (parsed && parsed.error) {
    throw new ApiError(
      parsed.error.message,
      parsed.error.code,
      response.status,
    );
  }

  throw new ApiError("request failed", "internal_error", response.status);
}

async function request<T>(
  method: "GET" | "POST",
  path: string,
  body?: unknown,
  token?: string,
): Promise<T> {
  let response: Response;

  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method,
      headers: buildHeaders(body !== undefined, token),
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });
  } catch {
    throw new ApiError("network error", "network_error", 0);
  }

  return handleResponse<T>(response);
}

export function apiPost<T>(
  path: string,
  body: unknown,
  token?: string,
): Promise<T> {
  return request<T>("POST", path, body, token);
}

export function apiGet<T>(path: string, token?: string): Promise<T> {
  return request<T>("GET", path, undefined, token);
}
