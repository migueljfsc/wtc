import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useTheme } from "@/components/ThemeProvider";
import { useAuth } from "@/auth/AuthProvider";
import { useVersion } from "@/lib/queries";
import { config } from "@/lib/config";
import { deleteFilter, loadSavedFilters } from "@/lib/savedFilters";

/** One label/value line, matching the Configuration tab's row style. */
function KV({ k, v, mono = true }: { k: string; v: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline gap-2 text-xs">
      <span className="w-32 shrink-0 text-muted-foreground">{k}</span>
      <span className={mono ? "font-mono break-all" : ""}>{v}</span>
    </div>
  );
}

function About() {
  const version = useVersion();
  const apiVersion = version.isLoading
    ? "…"
    : (version.data?.version ?? "unreachable");

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">About</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1.5">
        <KV k="API version" v={apiVersion} />
        <KV k="UI version" v={__WTC_UI_VERSION__} />
        <div className="flex items-baseline gap-2 text-xs">
          <span className="w-32 shrink-0 text-muted-foreground">project</span>
          <a
            className="font-mono underline-offset-2 hover:underline"
            href="https://github.com/migueljfsc/wtc"
            target="_blank"
            rel="noreferrer"
          >
            github.com/migueljfsc/wtc
          </a>
        </div>
      </CardContent>
    </Card>
  );
}

function Connection() {
  const version = useVersion();
  const status = version.isLoading
    ? "checking…"
    : version.error
      ? "unreachable or unauthorized"
      : "connected";

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">Connection</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1.5">
        <KV k="API endpoint" v={config.apiBaseUrl || "same origin"} />
        <KV k="status" v={status} mono={false} />
      </CardContent>
    </Card>
  );
}

const THEMES = ["light", "system", "dark"] as const;

function Appearance() {
  const { theme, setTheme } = useTheme();
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">Appearance</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex gap-2">
          {THEMES.map((t) => (
            <Button
              key={t}
              size="sm"
              variant={theme === t ? "default" : "outline"}
              onClick={() => setTheme(t)}
            >
              {t}
            </Button>
          ))}
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          Stored in this browser; “system” follows your OS preference.
        </p>
      </CardContent>
    </Card>
  );
}

function SessionData() {
  const { logout } = useAuth();
  const [filters, setFilters] = useState(() => loadSavedFilters());

  function clearFilters() {
    for (const f of filters) deleteFilter(f.name);
    setFilters([]);
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">Session &amp; local data</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            {filters.length === 0
              ? "No saved timeline filters in this browser."
              : `${filters.length} saved timeline filter(s) in this browser.`}
          </p>
          <Button size="sm" variant="outline" onClick={clearFilters} disabled={filters.length === 0}>
            Clear filters
          </Button>
        </div>
        <div className="flex items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            Logging out removes the API token from this browser.
          </p>
          <Button size="sm" variant="outline" onClick={logout}>
            Log out
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

export function Settings() {
  return (
    <div className="mx-auto max-w-4xl space-y-8">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">
          Versions, connection, and this browser’s preferences. Server-side
          configuration lives in the Configuration tab.
        </p>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <About />
        <Connection />
        <Appearance />
        <SessionData />
      </div>
    </div>
  );
}
