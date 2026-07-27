"use client"

// Re-export the shared ThemeProvider from @multica/ui
export { ThemeProvider } from "@multica/ui/components/common/theme-provider"

// Suppress React 19 false-positive about next-themes' inline <script>.
// The script works correctly; React 19 just warns about any <script> in components.
// See: https://github.com/pacocoursey/next-themes/issues/337
if (typeof window !== "undefined" && process.env.NODE_ENV === "development") {
  const orig = console.error;
  console.error = (...args: unknown[]) => {
    const text = args
      .filter((arg): arg is string => typeof arg === "string")
      .join(" ");
    // next-themes injects a script; React 19 warns, but the script is correct.
    if (text.includes("Encountered a script tag")) return;
    // Next.js 16 opens a full-screen console overlay on console.error that
    // intercepts all clicks. ApiClient already throws to callers — keep the
    // log as warn so failed optional requests (e.g. Elba 503) don't freeze UI.
    if (text.includes("[api]")) {
      console.warn(...args);
      return;
    }
    orig.apply(console, args);
  };
}
