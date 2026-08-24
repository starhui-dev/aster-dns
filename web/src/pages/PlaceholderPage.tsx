import { Alert, PageHeader, Panel } from "../components/ui/Layout";

export default function PlaceholderPage(props: {
  title: string;
  phase: string;
  description: string;
}) {
  return (
    <div class="space-y-6">
      <PageHeader eyebrow={props.phase} title={props.title} description={props.description} />
      <Panel>
        <Alert>
          This route is reserved for the real provider-backed implementation. No placeholder data or
          simulated success state is being served.
        </Alert>
      </Panel>
    </div>
  );
}
