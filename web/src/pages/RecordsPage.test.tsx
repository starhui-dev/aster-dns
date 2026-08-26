import { Route, Router } from "@solidjs/router";
import { fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthProvider } from "../app/AuthContext";
import type { RecordSet } from "../lib/dns";
import RecordsPage from "./RecordsPage";

const zoneID = "01900000-0000-7000-8000-000000000201";
let recordsets: RecordSet[];
let createShouldFail: boolean;
let updateShouldConflict: boolean;
let listShouldFail: boolean;
let lastUpdateBody: Record<string, unknown> | undefined;

beforeEach(() => {
  createShouldFail = false;
  updateShouldConflict = false;
  listShouldFail = false;
  lastUpdateBody = undefined;
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
    {
      id: "record-3",
      name: "batch.example.com",
      type: "A",
      ttl: 300,
      entries: [{ value: "192.0.2.30" }],
      extensions: { cloudflare: { proxied: false } },
      provider_version: "v1",
      fingerprint: "fingerprint-3",
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
  it("shows a provider conflict and reapplies against refreshed state", async () => {
    updateShouldConflict = true;
    vi.stubGlobal("fetch", vi.fn(fakeProviderFetch));

    render(() => (
      <AuthProvider>
        <Router>
          <Route path="/zones/:zoneId/records" component={RecordsPage} />
        </Router>
      </AuthProvider>
    ));

    expect(await screen.findByRole("heading", { name: "example.com" })).toBeInTheDocument();
    const row = screen.getByText("api.example.com").closest("tr");
    expect(row).not.toBeNull();
    fireEvent.click(within(row as HTMLTableRowElement).getByRole("button", { name: "Edit" }));
    fireEvent.input(await screen.findByLabelText("TTL (seconds)"), { target: { value: "600" } });
    fireEvent.click(screen.getByRole("button", { name: "Save record set" }));

    expect(await screen.findByText("Provider conflict")).toBeInTheDocument();
    expect(screen.getByText((text) => text.includes("192.0.2.99"))).toBeInTheDocument();

    updateShouldConflict = false;
    fireEvent.click(screen.getByRole("button", { name: "Reapply against current" }));
    expect(
      await screen.findByText("Changes reapplied against the current provider state."),
    ).toBeInTheDocument();
    expect(lastUpdateBody?.expected_fingerprint).toBe("server-fingerprint");
    expect(lastUpdateBody?.provider_version).toBe("server-version");
  });

  it("renders per-item partial batch results and request diagnostics", async () => {
    vi.stubGlobal("fetch", vi.fn(fakeProviderFetch));

    render(() => (
      <AuthProvider>
        <Router>
          <Route path="/zones/:zoneId/records" component={RecordsPage} />
        </Router>
      </AuthProvider>
    ));

    expect(await screen.findByRole("heading", { name: "example.com" })).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Select api.example.com A"));
    fireEvent.click(screen.getByLabelText("Select batch.example.com A"));
    fireEvent.click(screen.getByRole("button", { name: "Batch delete" }));
    fireEvent.input(await screen.findByLabelText("Type example.com to confirm"), {
      target: { value: "example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply to 2 items" }));

    expect(await screen.findByText("Batch result")).toBeInTheDocument();
    expect(screen.getByText("1 succeeded · 1 failed")).toBeInTheDocument();
    expect(
      screen.getByText((text) => text.includes("Provider changed before deletion.")),
    ).toBeInTheDocument();
    expect(screen.getByText((text) => text.includes("req_batch_conflict"))).toBeInTheDocument();
  });

  it("renders a safe Provider refresh error with its request ID", async () => {
    listShouldFail = true;
    vi.stubGlobal("fetch", vi.fn(fakeProviderFetch));

    render(() => (
      <AuthProvider>
        <Router>
          <Route path="/zones/:zoneId/records" component={RecordsPage} />
        </Router>
      </AuthProvider>
    ));

    expect(await screen.findByText("Provider unavailable.")).toBeInTheDocument();
    expect(screen.getByText("Request req_record_list")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Cached data is marked stale.");
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
    if (listShouldFail) {
      return jsonResponse(
        {
          error: {
            code: "upstream",
            message: "Provider unavailable.",
            request_id: "req_record_list",
          },
        },
        503,
      );
    }
    return jsonResponse({
      recordsets,
      total: recordsets.length,
      fetched_at: "2026-08-26T00:00:00Z",
      stale: false,
    });
  }
  if (path.endsWith(`/zones/${zoneID}/recordsets/batch`) && method === "POST") {
    const body = JSON.parse(String(init?.body)) as { items: Array<{ recordset_id: string }> };
    return jsonResponse(
      {
        succeeded: 1,
        failed: 1,
        items: [
          { id: body.items[0]?.recordset_id ?? "record-1", status: "succeeded" },
          {
            id: body.items[1]?.recordset_id ?? "record-3",
            status: "failed",
            error: {
              code: "conflict",
              message: "Provider changed before deletion.",
              request_id: "req_batch_conflict",
            },
          },
        ],
      },
      207,
    );
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
  if (path.includes(`/zones/${zoneID}/recordsets/`) && method === "PATCH") {
    const body = JSON.parse(String(init?.body)) as RecordSet & {
      expected_fingerprint?: string;
      provider_version?: string;
    };
    lastUpdateBody = body as unknown as Record<string, unknown>;
    const recordID = path.slice(path.lastIndexOf("/") + 1);
    const existing = recordsets.find((record) => record.id === recordID) ?? recordsets[0];
    if (updateShouldConflict && existing !== undefined) {
      const current: RecordSet = {
        ...existing,
        entries: [{ ...existing.entries[0], value: "192.0.2.99" }],
        provider_version: "server-version",
        fingerprint: "server-fingerprint",
      };
      return jsonResponse(
        {
          error: {
            code: "conflict",
            message: "The Provider record changed.",
            request_id: "req_conflict",
            details: { current },
          },
        },
        409,
      );
    }
    const updated: RecordSet = {
      id: recordID,
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
  if (path.includes(`/zones/${zoneID}/recordsets/`) && method === "DELETE") {
    const recordID = path.slice(path.lastIndexOf("/") + 1);
    recordsets = recordsets.filter((record) => record.id !== recordID);
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
