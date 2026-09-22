import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { SettingsPage } from "./SettingsPage";

const client = vi.hoisted(() => ({
  settings: vi.fn().mockResolvedValue(null),
  appVersion: vi.fn().mockResolvedValue("test"),
  diagnose: vi.fn().mockResolvedValue({ grok_path: "fixture", grok_version: "test", compatible: true, logged_in: true, leader_running: false, attach_mode: "boundary" }),
}));
vi.mock("../api", () => ({ getClient: () => client }));
vi.mock("../components/MCPInstallPanel", () => ({ MCPInstallPanel: () => null }));
vi.mock("../components/ThemeProvider", () => ({ useTheme: () => ({ appearance: "system", setAppearance: vi.fn() }) }));

let root: Root | undefined;
let container: HTMLDivElement;
afterEach(async () => {
  if (root) await act(async () => root?.unmount());
  container?.remove();
  vi.clearAllMocks();
});

it("does not run diagnostics on mount; only an explicit click invokes Grok diagnostics", async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => root?.render(<MemoryRouter><SettingsPage /></MemoryRouter>));
  expect(client.diagnose).not.toHaveBeenCalled();
  const button = [...container.querySelectorAll("button")].find((b) => b.textContent === "运行诊断");
  expect(button).toBeDefined();
  await act(async () => button?.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(client.diagnose).toHaveBeenCalledTimes(1);
});
