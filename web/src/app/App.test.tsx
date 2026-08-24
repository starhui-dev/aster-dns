import { render, screen } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the real API status and the Phase 1 security boundary", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              name: "Aster DNS",
              api_version: "v1",
              version: "test",
              commit: "test",
              status: "available",
            }),
            {
              status: 200,
              headers: {
                "Content-Type": "application/json",
                "X-Request-ID": "req_test",
              },
            },
          ),
        ),
      ),
    );

    render(() => <App />);

    expect(await screen.findByText("API connected")).toBeInTheDocument();
    expect(screen.getByText("Security initialization pending")).toBeInTheDocument();
    expect(screen.getByText("Not implemented")).toBeInTheDocument();
  });
});
