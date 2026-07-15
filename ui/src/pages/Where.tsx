import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { useWhere } from "@/lib/queries";
import { WherePipeline } from "@/components/where/WherePipeline";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";

export function Where() {
  // The ref lives in the URL (?ref=) so Diff cells can deep-link here.
  const [params, setParams] = useSearchParams();
  const ref = params.get("ref");
  const [input, setInput] = useState(ref ?? "");
  useEffect(() => setInput(ref ?? ""), [ref]);

  const where = useWhere(ref);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    const v = input.trim();
    setParams(v ? { ref: v } : {});
  }

  return (
    <div className="mx-auto max-w-3xl space-y-5">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Where</h1>
        <p className="text-sm text-muted-foreground">
          Trace a commit, tag or artifact from build through intent to each
          environment it reached.
        </p>
      </div>

      <form onSubmit={submit} className="flex gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="git sha, image tag, or artifact…"
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
        </div>
        <Button type="submit" disabled={!input.trim()}>
          Trace
        </Button>
      </form>

      {!ref && (
        <p className="text-sm text-muted-foreground">Enter a ref to see its journey.</p>
      )}
      {where.isLoading && <p className="text-sm text-muted-foreground">Resolving…</p>}
      {where.error && (
        <p className="text-sm text-destructive">
          Couldn’t resolve <code>{ref}</code>. Try a different sha or tag.
        </p>
      )}
      {where.data && <WherePipeline report={where.data} />}
    </div>
  );
}
