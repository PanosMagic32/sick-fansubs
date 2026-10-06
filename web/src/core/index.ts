import "./index.css";
import { registerServiceWorker } from "./service-worker.js";
import { watchInstallPrompt } from "@shared/pwa/install-prompt.js";
import "@features/auth/data-access/session.js";
import "@features/auth/sign-in-page.js";
import "@features/auth/register-page.js";
import "@features/auth/forgot-page.js";
import "@features/auth/reset-page.js";
import "@features/auth/verify-page.js";
import "@features/auth/account-page.js";
import "@features/blog/blog-list-page.js";
import "@features/blog/blog-detail-page.js";
import "@features/projects/projects-list-page.js";
import "@features/projects/project-detail-page.js";
import "@features/search/search-page.js";
import "@features/about/about-page.js";
import "@features/admin/admin-page.js";
import "@features/admin/admin-projects-page.js";
import "@features/admin/admin-users-page.js";
import "@features/admin/admin-metrics-page.js";
import "@features/admin/admin-logs-page.js";
import "@features/admin/blog-form-page.js";
import "@features/admin/project-form-page.js";
import "@core/shell/app-shell";
import "@core/shell/app-header";
import "@core/shell/app-footer";

// PWA: register the service worker in production only. The module
// script runs before DOMContentLoaded; waiting for the event keeps the
// worker's install from competing with the first page load.
window.addEventListener("DOMContentLoaded", () => {
  registerServiceWorker(import.meta.env.PROD, navigator);
});

// Install prompt: capture `beforeinstallprompt`
// from the first frame — the browser can fire it before the home-only
// donation strip renders the control that replays it (install-prompt.ts).
watchInstallPrompt(window);
