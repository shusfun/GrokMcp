export type Appearance = "system" | "light" | "dark";

export const APPEARANCE_KEY = "grok-supervisor.appearance";

const OPTIONS: Appearance[] = ["system", "light", "dark"];

export function isAppearance(value: string): value is Appearance {
  return OPTIONS.includes(value as Appearance);
}

export function readAppearance(): Appearance {
  try {
    const raw = localStorage.getItem(APPEARANCE_KEY) ?? "";
    return isAppearance(raw) ? raw : "system";
  } catch {
    return "system";
  }
}

export function writeAppearance(value: Appearance) {
  try {
    localStorage.setItem(APPEARANCE_KEY, value);
  } catch {
    /* ignore quota / private mode */
  }
}

export function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function resolveTheme(appearance: Appearance): "light" | "dark" {
  if (appearance === "light" || appearance === "dark") return appearance;
  return systemPrefersDark() ? "dark" : "light";
}

export function applyAppearance(appearance: Appearance) {
  document.documentElement.dataset.theme = resolveTheme(appearance);
}

export function bootTheme() {
  applyAppearance(readAppearance());
}
