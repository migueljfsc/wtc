import { useState } from "react";
import { Activity } from "lucide-react";
import { useAuth } from "@/auth/AuthProvider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { config } from "@/lib/config";

export function LoginPage() {
  const { login } = useAuth();
  const [token, setToken] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const ok = await login(token.trim());
      if (!ok) setError("Token rejected by the server.");
    } catch {
      setError(
        `Could not reach the wtc API at ${config.apiBaseUrl || "the current origin"}. Check the URL and CORS config.`,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-1 flex items-center gap-2">
            <Activity className="size-5 text-primary" />
            <span className="text-lg font-semibold tracking-tight">wtc portal</span>
          </div>
          <CardTitle className="text-xl">Sign in</CardTitle>
          <CardDescription>
            Enter an API token (a value from the server's{" "}
            <code className="text-xs">auth.api_tokens</code>).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-3">
            <Input
              type="password"
              autoFocus
              placeholder="API token"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              aria-label="API token"
            />
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" disabled={!token.trim() || busy}>
              {busy ? "Verifying…" : "Continue"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
