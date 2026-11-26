// Canonical public links come from configuration, never request or proxy headers.
export function siteOrigin() {
  const configured = process.env.SITE_ORIGIN;
  const mode = process.env.APP_ENV ?? (process.env.NODE_ENV === "production" ? "production" : "development");
  if (!["development", "production"].includes(mode)) throw new Error("APP_ENV must be production or development");
  if (!configured && mode === "production") throw new Error("SITE_ORIGIN is required in production");
  const url = new URL(configured ?? "http://127.0.0.1:3000");
  const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  if (url.username || url.password || url.search || url.hash || (url.pathname !== "/") ||
      (url.protocol !== "https:" && !(mode === "development" && url.protocol === "http:" && loopback))) {
    throw new Error("SITE_ORIGIN must be an HTTPS origin (or a development loopback origin)");
  }
  return url.origin;
}
