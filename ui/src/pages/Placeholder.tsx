import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

/**
 * Placeholder is the empty view stub for routes whose real view doesn't
 * exist yet, so navigation and layout can be exercised.
 */
export function Placeholder({
  title,
  phase,
  children,
}: {
  title: string;
  phase: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-4xl">
      <h1 className="mb-1 text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="mb-6 text-sm text-muted-foreground">Planned for {phase}.</p>
      <Card>
        <CardHeader>
          <CardTitle>Coming soon</CardTitle>
          <CardDescription>
            This view is a stub. The portal shell, auth and API wiring are in
            place; the real {title.toLowerCase()} experience arrives in {phase}.
          </CardDescription>
        </CardHeader>
        {children && <CardContent>{children}</CardContent>}
      </Card>
    </div>
  );
}
