import { GlobalRegistrator } from "@happy-dom/global-registrator";

// Tests never navigate for real — URLs move only through the history shim.
GlobalRegistrator.register({
  settings: {
    navigation: {
      disableMainFrameNavigation: true,
      disableFallbackToSetURL: true,
    },
  },
});

// Default browser preferences for testing:
// prefers-color-scheme defaults to light in happy-dom, but our app
// defaults to dark — mock matchMedia to reflect that.
const originalMatchMedia = window.matchMedia.bind(window);
window.matchMedia = (query: string) => {
  const result = originalMatchMedia(query);
  if (query === "(prefers-color-scheme: light)") {
    Object.defineProperty(result, "matches", { value: false });
  }
  if (query === "(prefers-reduced-motion: reduce)") {
    Object.defineProperty(result, "matches", { value: false });
  }
  return result;
};
