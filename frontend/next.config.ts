import type { NextConfig } from "next";

// Server-side only — never exposed to client JS (no NEXT_PUBLIC_ prefix).
const BACKEND_ORIGIN = process.env.BACKEND_ORIGIN ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  turbopack: {
    root: __dirname,
  },

  // Proxies every /api/* request through to the Go backend, so the browser
  // only ever talks to this frontend's own domain and the session cookie is
  // first-party. See docs/intent/account-access/ (ACCT-016).
  //
  // @spec ACCT-016, PLATFORM-020, DECK-030
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${BACKEND_ORIGIN}/api/:path*`,
      },
      // The public deck view is mounted at the router root on the backend,
      // deliberately outside the authenticated `/api` group (DECK-030), so it
      // needs its own same-origin rewrite.
      {
        source: "/public/:path*",
        destination: `${BACKEND_ORIGIN}/public/:path*`,
      },
    ];
  },
};

export default nextConfig;
