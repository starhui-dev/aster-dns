import { render, screen } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders authenticated security state and admin navigation", async () => {
    vi.stubGlobal("fetch", vi.fn(authenticatedFetch));

    render(() => <App />);

    expect(await screen.findByText("API connected")).toBeInTheDocument();
    expect(screen.getByText("Authentication security active")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Users" })).toHaveLength(2);
  });

  it("renders Passkey-first login with optional password fallback", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path.endsWith("/auth/session")) {
          return jsonResponse(
            {
              error: {
                code: "authentication_failed",
                message: "Authentication failed.",
                request_id: "req_unauthenticated",
              },
            },
            401,
          );
        }
        if (path.endsWith("/auth/bootstrap")) {
          return jsonResponse({
            required: false,
            configured: false,
            password_login_enabled: true,
          });
        }
        throw new Error(`Unexpected request: ${path}`);
      }),
    );

    render(() => <App />);

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue with Passkey" })).toBeInTheDocument();
    expect(screen.getByText("Password fallback")).toBeInTheDocument();
  });
});

async function authenticatedFetch(input: RequestInfo | URL): Promise<Response> {
  const path = String(input);
  if (path.endsWith("/auth/session")) {
    return jsonResponse({
      authenticated: true,
      password_login_enabled: true,
      user: {
        id: "01900000-0000-7000-8000-000000000001",
        username: "admin",
        display_name: "Administrator",
        role: "admin",
        password_enabled: false,
        totp_required: false,
        created_at: "2026-08-24T00:00:00Z",
        updated_at: "2026-08-24T00:00:00Z",
      },
    });
  }
  if (path.endsWith("/api/v1")) {
    return jsonResponse({
      name: "Aster DNS",
      api_version: "v1",
      version: "test",
      commit: "test",
      status: "available",
    });
  }
  throw new Error(`Unexpected request: ${path}`);
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_test" },
  });
}
