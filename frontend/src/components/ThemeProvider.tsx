import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  applyAppearance,
  readAppearance,
  resolveTheme,
  writeAppearance,
  type Appearance,
} from "../lib/theme";

type ThemeContextValue = {
  appearance: Appearance;
  resolved: "light" | "dark";
  setAppearance: (value: Appearance) => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [appearance, setAppearanceState] = useState<Appearance>(readAppearance);

  useEffect(() => {
    applyAppearance(appearance);
    writeAppearance(appearance);
    if (appearance !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => applyAppearance("system");
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [appearance]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      appearance,
      resolved: resolveTheme(appearance),
      setAppearance: setAppearanceState,
    }),
    [appearance],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
