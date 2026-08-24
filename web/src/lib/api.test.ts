import { afterEach, describe, expect, it, vi } from "vitest";

import { apiRequest } from "./api";

describe("apiRequest", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    document.cookie = "aster_csrf=; Max-Age=0; path=/";
  });

  it("maps the stable API error envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              error: {
                code: "not_found",
                message: "Missing resource.",
                request_id: "req_contract",
              },
            }),
            { status: 404, headers: { "Content-Type": "application/json" } },
          ),
        ),
      ),
    );

    await expect(apiRequest("/missing")).rejects.toMatchObject({
      code: "not_found",
      message: "Missing resource.",
      requestId: "req_contract",
      status: 404,
    });
  });

  it("adds the in-memory session CSRF cookie to mutations", async () => {
    document.cookie = "aster_csrf=csrf-test-token; path=/";
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      expect(headers.get("X-CSRF-Token")).toBe("csrf-test-token");
      expect(headers.get("Content-Type")).toBe("application/json");
      return new Response(JSON.stringify({ status: "ok" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await apiRequest("/mutation", { method: "POST", body: JSON.stringify({ value: true }) });

    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
