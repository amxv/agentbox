"use client";

import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState
} from "react";
import { fetchSession, type AuthContext } from "./session";

type PanelSessionState = {
  auth: AuthContext | null;
  loading: boolean;
  refresh: () => Promise<void>;
  clear: () => void;
};

const PanelSessionContext = createContext<PanelSessionState | null>(null);

let cachedAuth: AuthContext | null | undefined;
let pendingSession: Promise<AuthContext | null> | null = null;

function loadSession() {
  if (cachedAuth !== undefined) return Promise.resolve(cachedAuth);
  if (!pendingSession) {
    pendingSession = fetchSession()
      .then((auth) => {
        cachedAuth = auth;
        return auth;
      })
      .finally(() => {
        pendingSession = null;
      });
  }
  return pendingSession;
}

export function PanelSessionProvider({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthContext | null>(cachedAuth ?? null);
  const [loading, setLoading] = useState(cachedAuth === undefined);

  useEffect(() => {
    let active = true;
    void loadSession()
      .then((nextAuth) => {
        if (active) setAuth(nextAuth);
      })
      .catch(() => {
        if (active) setAuth(null);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const value = useMemo<PanelSessionState>(() => ({
    auth,
    loading,
    async refresh() {
      cachedAuth = undefined;
      setLoading(true);
      try {
        const nextAuth = await loadSession();
        setAuth(nextAuth);
      } finally {
        setLoading(false);
      }
    },
    clear() {
      cachedAuth = null;
      setAuth(null);
      setLoading(false);
    }
  }), [auth, loading]);

  return (
    <PanelSessionContext.Provider value={value}>
      {children}
    </PanelSessionContext.Provider>
  );
}

export function usePanelSession() {
  const context = useContext(PanelSessionContext);
  if (!context) throw new Error("usePanelSession must be used within PanelSessionProvider.");
  return context;
}
