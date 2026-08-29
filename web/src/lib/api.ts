const API_BASE = "/api/v1";
export interface ApiOverview {
  name: string;
  api_version: string;
  version: string;
  commit: string;
  status: string;
}
export interface UpdateCheckResult {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_url: string;
}

export function checkForUpdates(): Promise<UpdateCheckResult> {
  return apiRequest<UpdateCheckResult>("/updates");
}

export function getApiOverview(): Promise<ApiOverview> {
  return apiRequest<ApiOverview>("");
}

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
export function apiErrorMessage(error: ApiError, fallback: string): string {
  return error.code === "request_failed" && error.message === "The API request failed."
    ? fallback
    : error.message;
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
      try {
        return decodeURIComponent(value.slice(prefix.length));
      } catch {
        return null;
      }
    }
  }
  return null;
}

const clientSensitiveKey =
  /authorization|cookie|password|secret|token|credential|accesskey|apikey|ciphertext|nonce|privatekey/i;
const clientSensitiveValue =
  /((?:authorization|cookie|password|secret|token|credential|signature|access[_-]?key|api[_-]?key)[^:=\r\n]{0,24}[:=][ \t]*)(?:bearer[ \t]+|basic[ \t]+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)/gi;

export function redactClientValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redactClientValue);
  if (value !== null && typeof value === "object") {
    const safe: Record<string, unknown> = {};
    for (const [key, nested] of Object.entries(value)) {
      if (!clientSensitiveKey.test(key.replace(/[^a-z0-9]/gi, ""))) {
        safe[key] = redactClientValue(nested);
      }
    }
    return safe;
  }
  if (typeof value === "string") return value.replace(clientSensitiveValue, "$1[REDACTED]");
  return value;
}
