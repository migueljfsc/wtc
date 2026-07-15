import { createContext, useCallback, useContext, useMemo, useState } from "react";
import { verifyToken } from "@/lib/api";
import { clearToken, getToken, setToken } from "@/lib/token";

interface AuthState {
  isAuthenticated: boolean;
  /** Validates a token against the server, persisting it on success. */
  login: (token: string) => Promise<boolean>;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [authed, setAuthed] = useState<boolean>(() => getToken() !== null);

  const login = useCallback(async (token: string) => {
    const ok = await verifyToken(token);
    if (ok) {
      setToken(token);
      setAuthed(true);
    }
    return ok;
  }, []);

  const logout = useCallback(() => {
    clearToken();
    setAuthed(false);
  }, []);

  const value = useMemo<AuthState>(
    () => ({ isAuthenticated: authed, login, logout }),
    [authed, login, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}
