import { render, screen } from "@solidjs/testing-library";
import { afterEach, describe, expect, it, vi } from "vitest";

import ProviderAccountsPage from "./ProviderAccountsPage";

describe("ProviderAccountsPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders live provider capabilities without credential values", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          provider_types: [
            {
              type: "huawei",
              display_name: "Huawei Cloud DNS",
              documentation_url: "https://support.huaweicloud.com/intl/en-us/dns/index.html",
              credential_fields: [
                { key: "access_key", label: "Access key (AK)", secret: true, required: true },
                { key: "secret_key", label: "Secret key (SK)", secret: true, required: true },
                { key: "security_token", label: "Security token", secret: true, required: false },
              ],
              account_options: [
                { key: "region", label: "DNS region", secret: false, required: true },
              ],
              capabilities: {
                supported_record_types: ["A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"],
                min_ttl: 1,
                max_ttl: 2147483647,
                native_record_granularity: "record_set",
                supports_routing_line: true,
                supports_weight: true,
                supports_record_status: true,
              },
            },
            {
              type: "tencent",
              display_name: "Tencent Cloud DNSPod",
              documentation_url: "https://cloud.tencent.com/document/product/1427",
              credential_fields: [
                { key: "secret_id", label: "SecretId", secret: true, required: true },
                { key: "secret_key", label: "SecretKey", secret: true, required: true },
                { key: "token", label: "Security token", secret: true, required: false },
              ],
              account_options: [],
              capabilities: {
                supported_record_types: ["A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"],
                min_ttl: 1,
                max_ttl: 604800,
                native_record_granularity: "record_entry",
                supports_routing_line: true,
                supports_weight: true,
                supports_record_status: true,
              },
            },
          ],
        }),
      ),
    );

    render(() => <ProviderAccountsPage />);

    expect(await screen.findByRole("heading", { name: "Huawei Cloud DNS" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Tencent Cloud DNSPod" })).toBeInTheDocument();
    expect(screen.getByText("RRSet")).toBeInTheDocument();
    expect(screen.getByText("Record entry")).toBeInTheDocument();
    expect(screen.getByText("1–604800 seconds")).toBeInTheDocument();
    expect(screen.getAllByText("Supported")).toHaveLength(6);
    expect(screen.getByText("Access key (AK)")).toBeInTheDocument();
    expect(screen.getByText("SecretId")).toBeInTheDocument();
    expect(screen.getAllByText("Security token")).toHaveLength(2);
    expect(screen.getByText("DNS region")).toBeInTheDocument();
    expect(screen.queryByText("fixture-secret-key")).not.toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Official documentation" })).toHaveLength(2);
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_provider_catalog" },
  });
}
