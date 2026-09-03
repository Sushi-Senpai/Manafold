import { NextResponse } from "next/server";

// Named `proxy.ts`, not `middleware.ts` — Next.js 16 renamed Middleware to
// Proxy. This is a deliberate pass-through: a session-cookie presence check
// here cannot tell a valid session from an expired or revoked one, which is
// exactly what the client-side 401 handler in `lib/api.ts` already does
// correctly (ACCT-015). Route protection for `(app)/*` is enforced there.
//
// @spec PLATFORM-022, ACCT-013
export function proxy() {
  return NextResponse.next();
}

export const config = {
  matcher: ["/decks/:path*"],
};
