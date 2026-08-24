import { afterEach, describe, expect, it, vi } from "vitest";

import { apiRequest } from "./api";

describe("apiRequest", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
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
});
