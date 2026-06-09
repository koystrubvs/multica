/**
 * Server-side proxy for the SitePing bridge's bulk connection-status endpoint.
 * Same auth model as the sibling token/site-url routes: re-validate the caller's
 * Multica session against the Go backend, then forward to the bridge with the
 * admin key (which never leaves the container). Powers the projects-list
 * "SitePing" column.
 */
import { NextRequest, NextResponse } from "next/server";

const BRIDGE_URL = process.env.SITEPING_BRIDGE_URL || "http://siteping-bridge:7979";
const ADMIN_KEY = process.env.SITEPING_ADMIN_KEY || "";
const BACKEND_URL = process.env.REMOTE_API_URL || "http://backend:8080";

async function assertMulticaSession(req: NextRequest): Promise<NextResponse | null> {
  const cookie = req.headers.get("cookie") || "";
  if (!cookie) return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  try {
    const resp = await fetch(`${BACKEND_URL}/api/me`, { headers: { cookie }, cache: "no-store" });
    if (resp.status !== 200) return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  } catch (err) {
    return NextResponse.json(
      { error: "auth check failed", detail: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
  return null;
}

export async function GET(req: NextRequest) {
  const unauth = await assertMulticaSession(req);
  if (unauth) return unauth;
  if (!ADMIN_KEY) {
    return NextResponse.json({ error: "SITEPING_ADMIN_KEY not configured" }, { status: 500 });
  }
  try {
    const resp = await fetch(`${BRIDGE_URL}/admin/siteping-status`, {
      headers: { "X-Siteping-Admin-Key": ADMIN_KEY },
      cache: "no-store",
    });
    const body = await resp.text();
    return new NextResponse(body, { status: resp.status, headers: { "Content-Type": "application/json" } });
  } catch (err) {
    return NextResponse.json(
      { error: "bridge unreachable", detail: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
