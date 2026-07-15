import { createContext, useContext, useEffect, useMemo, useState } from "react";

type Theme = "dark" | "light" | "system";

interface ThemeState {
  theme: Theme;
  setTheme: (t: Theme) => void;
}

const ThemeContext = createContext<ThemeState | null>(null);
const STORAGE_KEY = "wtc.theme";

function apply(theme: Theme) {
  const root = window.document.documentElement;
  root.classList.remove("light", "dark");
  const resolved =
    theme === "system"
      ? window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;
  root.classList.add(resolved);
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(
    () => (localStorage.getItem(STORAGE_KEY) as Theme) || "system",
  );

  useEffect(() => {
    apply(theme);
  }, [theme]);

  const value = useMemo<ThemeState>(
    () => ({
      theme,
      setTheme: (t: Theme) => {
        localStorage.setItem(STORAGE_KEY, t);
        setThemeState(t);
      },
    }),
    [theme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useTheme(): ThemeState {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return ctx;
}
