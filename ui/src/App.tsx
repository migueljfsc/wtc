import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "@/auth/AuthProvider";
import { AppShell } from "@/components/AppShell";
import { LoginPage } from "@/auth/LoginPage";

// Route-level code splitting: each page is its own chunk, so heavy deps
// (Recharts in the dashboard) don't weigh down first load or the other views.
const Dashboard = lazy(() => import("@/pages/Dashboard").then((m) => ({ default: m.Dashboard })));
const Timeline = lazy(() => import("@/pages/Timeline").then((m) => ({ default: m.Timeline })));
const Changes = lazy(() => import("@/pages/Changes").then((m) => ({ default: m.Changes })));
const Where = lazy(() => import("@/pages/Where").then((m) => ({ default: m.Where })));
const Diff = lazy(() => import("@/pages/Diff").then((m) => ({ default: m.Diff })));
const Services = lazy(() => import("@/pages/Services").then((m) => ({ default: m.Services })));
const Configuration = lazy(() => import("@/pages/Configuration").then((m) => ({ default: m.Configuration })));
const Settings = lazy(() => import("@/pages/Settings").then((m) => ({ default: m.Settings })));

function PageFallback() {
  return <p className="p-6 text-sm text-muted-foreground">Loading…</p>;
}

export function App() {
  const { isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route
            index
            element={
              <Suspense fallback={<PageFallback />}>
                <Dashboard />
              </Suspense>
            }
          />
          <Route path="timeline" element={<Suspense fallback={<PageFallback />}><Timeline /></Suspense>} />
          <Route path="changes" element={<Suspense fallback={<PageFallback />}><Changes /></Suspense>} />
          <Route path="where" element={<Suspense fallback={<PageFallback />}><Where /></Suspense>} />
          <Route path="diff" element={<Suspense fallback={<PageFallback />}><Diff /></Suspense>} />
          <Route path="services" element={<Suspense fallback={<PageFallback />}><Services /></Suspense>} />
          <Route path="configuration" element={<Suspense fallback={<PageFallback />}><Configuration /></Suspense>} />
          <Route path="settings" element={<Suspense fallback={<PageFallback />}><Settings /></Suspense>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
