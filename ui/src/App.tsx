import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "@/auth/AuthProvider";
import { AppShell } from "@/components/AppShell";
import { LoginPage } from "@/auth/LoginPage";
import { Dashboard } from "@/pages/Dashboard";
import { Timeline } from "@/pages/Timeline";
import { Where } from "@/pages/Where";
import { Diff } from "@/pages/Diff";
import { Services } from "@/pages/Services";
import { Settings } from "@/pages/Settings";

export function App() {
  const { isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<Dashboard />} />
          <Route path="timeline" element={<Timeline />} />
          <Route path="where" element={<Where />} />
          <Route path="diff" element={<Diff />} />
          <Route path="services" element={<Services />} />
          <Route path="settings" element={<Settings />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
