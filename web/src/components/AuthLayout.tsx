import { type ParentProps } from "solid-js";

import { Brand } from "./Brand";

export function AuthLayout(
  props: ParentProps<{ eyebrow: string; title: string; description: string; wide?: boolean }>,
) {
  return (
    <main class="grid min-h-screen bg-background text-foreground lg:grid-cols-[minmax(20rem,1fr)_minmax(30rem,42rem)]">
      <section class="relative hidden overflow-hidden border-r border-sidebar-border bg-sidebar p-10 lg:flex lg:flex-col">
        <Brand />
        <div class="my-auto max-w-lg py-16">
          <p class="text-xs font-semibold text-primary">Unified DNS operations</p>
          <h2 class="mt-3 text-3xl font-semibold leading-tight tracking-tight">
            Manage provider accounts, zones, and records from one trusted control plane.
          </h2>
          <p class="mt-4 text-sm leading-7 text-muted-foreground">
            Provider state remains authoritative. Credentials stay server-side, mutations are
            role-checked, and every DNS change is auditable.
          </p>
        </div>
        <p class="text-xs text-muted-foreground">Aster DNS · A Starhui product</p>
      </section>

      <section class="flex min-h-screen items-center justify-center p-5 sm:p-8 lg:p-12">
        <div class={props.wide ? "w-full max-w-xl" : "w-full max-w-md"}>
          <div class="mb-8 lg:hidden">
            <Brand />
          </div>
          <div class="mb-6">
            <p class="text-xs font-semibold text-primary">{props.eyebrow}</p>
            <h1 class="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">{props.title}</h1>
            <p class="mt-2 text-sm leading-6 text-muted-foreground">{props.description}</p>
          </div>
          {props.children}
        </div>
      </section>
    </main>
  );
}
