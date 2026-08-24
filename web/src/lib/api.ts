const API_BASE = "/api/v1";

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
    request_id?: string;
    details?: Record<string, unknown>;
  };
}

export class ApiError extends Error {
  readonly code: string;
  readonly requestId: string | null;
  readonly status: number;
  readonly details: Record<string, unknown> | undefined;

  constructor(
    message: string,
    options: {
      code: string;
      requestId: string | null;
      status: number;
      details?: Record<string, unknown>;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.code = options.code;
    this.requestId = options.requestId;
    this.status = options.status;
    this.details = options.details;
  }
}

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const normalizedPath = path === "" ? "" : path.startsWith("/") ? path : `/${path}`;
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const method = (init.method ?? "GET").toUpperCase();
  if (init.body !== undefined && typeof init.body === "string" && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (!isSafeMethod(method)) {
    const csrfToken = readCookie("__Host-aster_csrf") ?? readCookie("aster_csrf");
    if (csrfToken !== null) {
      headers.set("X-CSRF-Token", csrfToken);
    }
  }

  const response = await fetch(`${API_BASE}${normalizedPath}`, {
    ...init,
    credentials: "same-origin",
    headers,
  });
  const requestId = response.headers.get("X-Request-ID");
  const payload = await readJSON(response);

  if (!response.ok) {
    const envelope = payload as ErrorEnvelope | null;
    throw new ApiError(envelope?.error?.message ?? "The API request failed.", {
      code: envelope?.error?.code ?? "request_failed",
      requestId: envelope?.error?.request_id ?? requestId,
      status: response.status,
      ...(envelope?.error?.details ? { details: envelope.error.details } : {}),
    });
  }

  return payload as T;
}

async function readJSON(response: Response): Promise<unknown> {
  const contentType = response.headers.get("Content-Type") ?? "";
  if (!contentType.includes("application/json")) {
    return null;
  }
  try {
    return await response.json();
  } catch {
    return null;
  }
}

function isSafeMethod(method: string): boolean {
  return method === "GET" || method === "HEAD" || method === "OPTIONS";
}

function readCookie(name: string): string | null {
  if (typeof document === "undefined") {
    return null;
  }
  const prefix = `${name}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix)) {
      return decodeURIComponent(value.slice(prefix.length));
    }
  }
  return null;
}
