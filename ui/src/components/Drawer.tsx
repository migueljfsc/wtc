import { useEffect } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";

/**
 * Lightweight right-side slide-over. Hand-built (no Radix dependency): closes
 * on Escape or overlay click, locks body scroll, and traps initial focus on the
 * close button. Sufficient for the event-detail drawer.
 */
export function Drawer({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50">
      <div
        className="absolute inset-0 bg-black/50 animate-in fade-in"
        onClick={onClose}
        aria-hidden
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="absolute right-0 top-0 flex h-full w-full max-w-xl flex-col border-l bg-background shadow-xl animate-in slide-in-from-right"
      >
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="truncate text-sm font-semibold">{title}</h2>
          <Button variant="ghost" size="icon" aria-label="Close" autoFocus onClick={onClose}>
            <X />
          </Button>
        </div>
        <div className="flex-1 overflow-auto p-4">{children}</div>
      </div>
    </div>
  );
}
