import { StrictMode, lazy, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { createHashRouter, RouterProvider } from "react-router";
import { App } from "./App";
import { ThemeProvider } from "./components/ThemeProvider";
import { bootTheme } from "./lib/theme";
import "./styles.css";

bootTheme();

const ProjectsPage = lazy(() => import("./pages/ProjectsPage").then((m) => ({ default: m.ProjectsPage })));
const ProjectDetailPage = lazy(() => import("./pages/ProjectDetailPage").then((m) => ({ default: m.ProjectDetailPage })));
const SessionPage = lazy(() => import("./pages/SessionPage").then((m) => ({ default: m.SessionPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((m) => ({ default: m.SettingsPage })));
const DiagnosticsPage = lazy(() => import("./pages/DiagnosticsPage").then((m) => ({ default: m.DiagnosticsPage })));

const router = createHashRouter([
  {
    path: "/",
    element: <App />,
    children: [
      { index: true, element: <Suspense fallback={null}><ProjectsPage /></Suspense> },
      { path: "projects/:projectId", element: <Suspense fallback={null}><ProjectDetailPage /></Suspense> },
      { path: "sessions/:jobId", element: <Suspense fallback={null}><SessionPage /></Suspense> },
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
