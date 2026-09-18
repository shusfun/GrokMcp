import { StrictMode, lazy, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { createHashRouter, RouterProvider } from "react-router";
import { App } from "./App";
import { ThemeProvider } from "./components/ThemeProvider";
import { bootTheme } from "./lib/theme";
import "./styles.css";

bootTheme();

const WorkbenchPage = lazy(() => import("./pages/WorkbenchPage").then((m) => ({ default: m.WorkbenchPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((m) => ({ default: m.SettingsPage })));
const DiagnosticsPage = lazy(() => import("./pages/DiagnosticsPage").then((m) => ({ default: m.DiagnosticsPage })));

const workbench = (
  <Suspense fallback={null}>
    <WorkbenchPage />
  </Suspense>
);

const router = createHashRouter([
  {
    path: "/",
    element: <App />,
    children: [
      {
        element: workbench,
        children: [
          { index: true },
          { path: "sessions/:jobId" },
        ],
      },
      { path: "settings", element: <Suspense fallback={null}><SettingsPage /></Suspense> },
      { path: "diagnostics", element: <Suspense fallback={null}><DiagnosticsPage /></Suspense> },
    ],
  },
]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
    </ThemeProvider>
  </StrictMode>,
);
