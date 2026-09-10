import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { api, unwrap, setToken, getToken, setUnauthorizedHandler, type User } from '../api/client';

type AuthState = {
  user: User | null;
  /** true until the stored token (if any) has been validated via /auth/me. */
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
};

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const logout = useCallback(() => {
    setToken(null);
    setUser(null);
  }, []);

  // Global 401 handler: any expired/invalid token logs the user out.
  useEffect(() => {
    setUnauthorizedHandler(() => logout());
    return () => setUnauthorizedHandler(null);
  }, [logout]);

  // Validate a persisted token on boot.
  useEffect(() => {
    if (!getToken()) {
      setLoading(false);
      return;
    }
    unwrap(api.GET('/auth/me'))
      .then((me) => setUser(me))
      .catch(() => setToken(null))
      .finally(() => setLoading(false));
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    const res = await unwrap(api.POST('/auth/login', { body: { username, password } }));
    setToken(res.token);
    setUser(res.user);
  }, []);

  return <AuthContext.Provider value={{ user, loading, login, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
