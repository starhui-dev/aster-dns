export default function PlaceholderPage(props: {
  title: string;
  phase: string;
  description: string;
}) {
  return (
    <section class="rounded-3xl border border-dashed border-slate-300 bg-white p-8 dark:border-slate-700 dark:bg-slate-900">
      <p class="text-sm font-semibold text-cyan-700 dark:text-cyan-300">{props.phase}</p>
      <h2 class="mt-2 text-3xl font-semibold tracking-tight">{props.title}</h2>
      <p class="mt-3 max-w-2xl text-sm leading-6 text-slate-600 dark:text-slate-300">
        {props.description}
      </p>
      <p class="mt-6 inline-flex rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
        No placeholder data is being served.
      </p>
    </section>
  );
}
