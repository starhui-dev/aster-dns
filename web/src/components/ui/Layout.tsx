import { Show, type JSX, type ParentProps } from "solid-js";

export function PageHeader(props: {
  eyebrow?: string | undefined;
  title: string;
  description?: string | undefined;
  actions?: JSX.Element | undefined;
}) {
  return (
    <header class="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-end sm:justify-between">
      <div class="min-w-0">
        <Show when={props.eyebrow}>
          <p class="text-xs font-semibold text-primary">{props.eyebrow}</p>
        </Show>
        <h2 class="mt-1 text-2xl font-semibold tracking-tight text-foreground">{props.title}</h2>
        <Show when={props.description}>
          <p class="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{props.description}</p>
        </Show>
      </div>
      <Show when={props.actions}>
        <div class="flex shrink-0 flex-wrap items-center gap-2">{props.actions}</div>
      </Show>
    </header>
  );
}

export function Panel(
  props: ParentProps<{
    title?: string | undefined;
    description?: string | undefined;
    class?: string | undefined;
    compact?: boolean | undefined;
  }>,
) {
  return (
    <section
      class={[
        "rounded-lg border border-border bg-surface shadow-sm",
        props.compact ? "p-4" : "p-5 sm:p-6",
        props.class,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <Show when={props.title || props.description}>
        <header class="mb-5">
          <Show when={props.title}>
            <h3 class="text-base font-semibold text-foreground">{props.title}</h3>
          </Show>
          <Show when={props.description}>
            <p class="mt-1 text-sm leading-6 text-muted-foreground">{props.description}</p>
          </Show>
        </header>
      </Show>
      {props.children}
    </section>
  );
}

export type AlertVariant = "info" | "success" | "warning" | "danger";

const alertClasses: Record<AlertVariant, string> = {
  info: "border-primary/25 bg-primary-subtle text-primary-subtle-foreground",
  success: "border-success/25 bg-success-surface text-success-foreground",
  warning: "border-warning/25 bg-warning-surface text-warning-foreground",
  danger: "border-danger/25 bg-danger-surface text-danger-foreground",
};

export function Alert(
  props: ParentProps<{
    variant?: AlertVariant | undefined;
    title?: string | undefined;
    class?: string | undefined;
    role?: "alert" | "status" | undefined;
  }>,
) {
  const variant = () => props.variant ?? "info";

  return (
    <div
      class={["rounded-md border px-4 py-3 text-sm leading-6", alertClasses[variant()], props.class]
        .filter(Boolean)
        .join(" ")}
      role={props.role ?? (variant() === "danger" ? "alert" : "status")}
    >
      <Show when={props.title}>
        <p class="font-semibold">{props.title}</p>
      </Show>
      <div class={props.title ? "mt-1" : undefined}>{props.children}</div>
    </div>
  );
}

export function Field(
  props: ParentProps<{
    label: string;
    for: string;
    hint?: string | undefined;
    class?: string | undefined;
  }>,
) {
  return (
    <div class={props.class}>
      <label class="mb-1.5 block text-sm font-medium text-foreground" for={props.for}>
        {props.label}
      </label>
      {props.children}
      <Show when={props.hint}>
        <p class="mt-1.5 text-xs leading-5 text-muted-foreground">{props.hint}</p>
      </Show>
    </div>
  );
}

export function Badge(
  props: ParentProps<{
    tone?: "neutral" | "primary" | "success" | "warning" | "danger" | undefined;
  }>,
) {
  const toneClasses = {
    neutral: "bg-muted text-muted-foreground",
    primary: "bg-primary-subtle text-primary-subtle-foreground",
    success: "bg-success-surface text-success-foreground",
    warning: "bg-warning-surface text-warning-foreground",
    danger: "bg-danger-surface text-danger-foreground",
  } as const;

  return (
    <span
      class={`inline-flex min-h-6 items-center rounded-full px-2.5 py-0.5 text-xs font-semibold ${toneClasses[props.tone ?? "neutral"]}`}
    >
      {props.children}
    </span>
  );
}
