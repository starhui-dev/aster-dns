import { Dialog as KobalteDialog } from "@kobalte/core/dialog";
import { Show, createEffect, type JSX, type ParentProps } from "solid-js";

export function ModalDialog(
  props: ParentProps<{
    open: boolean;
    onOpenChange: (open: boolean) => void;
    class?: string | undefined;
    fullScreen?: boolean | undefined;
    title?: JSX.Element | undefined;
  }>,
) {
  let previouslyFocused: HTMLElement | null = null;

  createEffect(() => {
    if (!props.open || previouslyFocused !== null) return;
    const active = document.activeElement;
    previouslyFocused = active instanceof HTMLElement ? active : null;
  });

  return (
    <KobalteDialog open={props.open} onOpenChange={props.onOpenChange} modal>
      <KobalteDialog.Portal>
        <KobalteDialog.Overlay class="fixed inset-0 z-50 bg-black/55 backdrop-blur-[2px]" />
        <div
          class={
            props.fullScreen
              ? "fixed inset-0 z-50 flex"
              : "fixed inset-0 z-50 flex items-center justify-center p-4"
          }
        >
          <KobalteDialog.Content
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              previouslyFocused?.focus();
              previouslyFocused = null;
            }}
            class={[
              "max-h-[94dvh] w-[min(56rem,calc(100vw-2rem))] overflow-y-auto rounded-lg border border-border bg-surface p-0 text-foreground shadow-2xl",
              props.class,
            ]
              .filter(Boolean)
              .join(" ")}
          >
            <Show when={props.title}>
              <KobalteDialog.Title class="sr-only">{props.title}</KobalteDialog.Title>
            </Show>
            {props.children}
          </KobalteDialog.Content>
        </div>
      </KobalteDialog.Portal>
    </KobalteDialog>
  );
}
