import lightMark from "../assets/brand/starhui-mark-color.svg";
import darkMark from "../assets/brand/starhui-mark-dark.svg";

export function Brand(props: { compact?: boolean }) {
  return (
    <div class="flex items-center gap-3" aria-label="Aster DNS by Starhui">
      <span class="relative block h-9 w-10 shrink-0" aria-hidden="true">
        <img class="h-9 w-10 object-contain dark:hidden" src={lightMark} alt="" />
        <img class="hidden h-9 w-10 object-contain dark:block" src={darkMark} alt="" />
      </span>
      {!props.compact && (
        <span class="min-w-0">
          <span class="block truncate text-sm font-semibold text-foreground">Aster DNS</span>
          <span class="block text-xs text-muted-foreground">A Starhui product</span>
        </span>
      )}
    </div>
  );
}
