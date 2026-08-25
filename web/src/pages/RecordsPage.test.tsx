import { Route, Router } from "@solidjs/router";
import { fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthProvider } from "../app/AuthContext";
import type { RecordSet } from "../lib/dns";
import RecordsPage from "./RecordsPage";

const zoneID = "01900000-0000-7000-8000-000000000201";
let recordsets: RecordSet[];
let createShouldFail: boolean;

beforeEach(() => {
  createShouldFail = false;
  recordsets = [
    {
      id: "record-1",
      name: "api.example.com",
      type: "A",
      ttl: 300,
      entries: [{ value: "192.0.2.10" }],
      extensions: { cloudflare: { proxied: false } },
      provider_version: "v1",
      fingerprint: "fingerprint-1",
    },
  ];
  window.history.replaceState({}, "", `/zones/${zoneID}/records`);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("RecordsPage", () => {
  it("runs create, update, and delete through a fake Provider API", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.stubGlobal("fetch", vi.fn(fakeProviderFetch));

    render(() => (
      <AuthProvider>
        <Router>
          <Route path="/zones/:zoneId/records" component={RecordsPage} />
        </Router>
      </AuthProvider>
    ));

    expect(await screen.findByRole("heading", { name: "example.com" })).toBeInTheDocument();
    expect(screen.getByText("192.0.2.10")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add record" }));
    fireEvent.input(await screen.findByLabelText("Name"), { target: { value: "www.example.com" } });
    fireEvent.input(screen.getByLabelText("Value"), { target: { value: "192.0.2.20" } });
    fireEvent.click(screen.getByLabelText("Proxied"));
    fireEvent.click(screen.getByRole("button", { name: "Save record set" }));

    expect(await screen.findByText("www.example.com")).toBeInTheDocument();
    expect(screen.getByText("192.0.2.20")).toBeInTheDocument();
    expect(screen.getByText("Yes")).toBeInTheDocument();

    const createdRow = screen.getByText("www.example.com").closest("tr");
    expect(createdRow).not.toBeNull();
    fireEvent.click(
      within(createdRow as HTMLTableRowElement).getByRole("button", { name: "Edit" }),
    );
    fireEvent.input(await screen.findByLabelText("TTL (seconds)"), { target: { value: "600" } });
    fireEvent.click(screen.getByRole("button", { name: "Save record set" }));

    await waitFor(() => {
      const updatedRow = screen.getByText("www.example.com").closest("tr");
      expect(within(updatedRow as HTMLTableRowElement).getByText("600s")).toBeInTheDocument();
    });

    const updatedRow = screen.getByText("www.example.com").closest("tr");
    fireEvent.click(
      within(updatedRow as HTMLTableRowElement).getByRole("button", { name: "Delete" }),
    );

    await waitFor(() => {
      expect(screen.queryByText("www.example.com")).not.toBeInTheDocument();
    });
  });

  it("restores focus inside the record editor after a server validation error", async () => {
    createShouldFail = true;
    vi.stubGlobal("fetch", vi.fn(fakeProviderFetch));

    render(() => (
      <AuthProvider>
        <Router>
          <Route path="/zones/:zoneId/records" component={RecordsPage} />
        </Router>
      </AuthProvider>
    ));

    expect(await screen.findByRole("heading", { name: "example.com" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Add record" }));
    const name = await screen.findByLabelText("Name");
    fireEvent.input(name, { target: { value: "invalid.example.com" } });
    fireEvent.input(screen.getByLabelText("Value"), { target: { value: "not-an-ip" } });
    const save = screen.getByRole("button", { name: "Save record set" });
    save.focus();
    fireEvent.click(save);

    expect(await screen.findByRole("alert")).toHaveTextContent("The record value is invalid.");
    await waitFor(() => expect(document.activeElement).toBe(name));
  });
});

async function fakeProviderFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const path = String(input);
  const method = init?.method ?? "GET";
  if (path.endsWith("/auth/session")) {
    return jsonResponse({
      authenticated: true,
      password_login_enabled: false,
      user: {
        id: "01900000-0000-7000-8000-000000000001",
        username: "operator",
        display_name: "DNS Operator",
        role: "operator",
        password_enabled: false,
        totp_required: false,
        created_at: "2026-08-24T00:00:00Z",
        updated_at: "2026-08-24T00:00:00Z",
      },
    });
  }
  if (path.endsWith(`/zones/${zoneID}`)) {
    return jsonResponse({ zone: zoneFixture });
  }
  if (path.endsWith("/provider-types")) {
    return jsonResponse({ provider_types: [cloudflareDefinition] });
  }
  if (path.includes(`/zones/${zoneID}/recordsets?`)) {
    return jsonResponse({
      recordsets,
      total: recordsets.length,
      fetched_at: "2026-08-26T00:00:00Z",
      stale: false,
    });
  }
  if (path.endsWith(`/zones/${zoneID}/recordsets`) && method === "POST") {
    if (createShouldFail) {
      return jsonResponse(
        {
          error: {
            code: "validation",
            message: "The record value is invalid.",
            request_id: "req_invalid_record",
          },
        },
        422,
      );
    }
    const body = JSON.parse(String(init?.body)) as {
      name: string;
      type: string;
      ttl: number;
      entries: RecordSet["entries"];
      extensions?: RecordSet["extensions"];
    };
    const created: RecordSet = {
      id: "record-2",
      ...body,
      provider_version: "v1",
      fingerprint: "fingerprint-2",
    };
    recordsets = [...recordsets, created];
    return jsonResponse({ recordset: created }, 201);
  }
  if (path.endsWith(`/zones/${zoneID}/recordsets/record-2`) && method === "PATCH") {
    const body = JSON.parse(String(init?.body)) as RecordSet;
    const updated: RecordSet = {
      id: "record-2",
      name: body.name,
      type: body.type,
      ttl: body.ttl,
      entries: body.entries,
      extensions: body.extensions,
      provider_version: "v2",
      fingerprint: "fingerprint-3",
    };
    recordsets = recordsets.map((record) => (record.id === updated.id ? updated : record));
    return jsonResponse({ recordset: updated });
  }
  if (path.endsWith(`/zones/${zoneID}/recordsets/record-2`) && method === "DELETE") {
    recordsets = recordsets.filter((record) => record.id !== "record-2");
    return new Response(null, { status: 204, headers: { "X-Request-ID": "req_delete" } });
  }
  throw new Error(`Unexpected request: ${method} ${path}`);
}

const zoneFixture = {
  id: zoneID,
  provider_account_id: "01900000-0000-7000-8000-000000000101",
  provider_type: "cloudflare",
  provider_account_name: "Cloudflare production",
  account_enabled: true,
  validation_status: "valid",
  name: "example.com",
  status: "active",
  metadata: { nameservers: ["ns1.example.net", "ns2.example.net"] },
  fetched_at: "2026-08-26T00:00:00Z",
  stale: false,
};

const cloudflareDefinition = {
  type: "cloudflare",
  display_name: "Cloudflare DNS",
  credential_fields: [],
  account_options: [],
  capabilities: {
    supported_record_types: ["A", "AAAA", "CNAME", "TXT", "MX"],
    min_ttl: 60,
    max_ttl: 86400,
    native_record_granularity: "record_entry",
    supports_proxy: true,
    supports_routing_line: false,
    supports_weight: false,
    supports_record_status: false,
    supports_dnssec: true,
    supports_native_batch: true,
    supports_comments: true,
    extension_fields: [
      {
        namespace: "cloudflare",
        scope: "record_set",
        key: "proxied",
        label: "Proxied",
        type: "boolean",
        read_only: false,
        required: false,
      },
    ],
  },
};

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": "req_fake_provider" },
  });
}
