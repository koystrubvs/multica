/**
 * Server-side proxy for token CRUD on the SitePing bridge.
 *
 * Auth model:
 *   1. We are a Next.js route handler, NOT a Go backend route — Multica's
 *      auth middleware does NOT see us. Without our own check, the proxy
 *      would be reachable from the public internet.
 *   2. So before forwarding to the bridge, we re-issue the caller's cookies
 *      against the Go backend's `/api/me`. If `/api/me` returns 200, the
 *      caller has a valid Multica session — anyone else gets a 401.
 *   3. The bridge admin key never leaves the Multica container.
 */
import { NextRequest, NextResponse } from "next/server";

const BRIDGE_URL = process.env.SITEPING_BRIDGE_URL || "http://siteping-bridge:7979";
const ADMIN_KEY = process.env.SITEPING_ADMIN_KEY || "";
const BACKEND_URL = process.env.REMOTE_API_URL || "http://backend:8080";

async function assertMulticaSession(req: NextRequest): Promise<NextResponse | null> {
  const cookie = req.headers.get("cookie") || "";
  if (!cookie) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }
  try {
    const resp = await fetch(`${BACKEND_URL}/api/me`, {
      headers: { cookie },
      cache: "no-store",
    });
    if (resp.status !== 200) {
      return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
    }
  } catch (err) {
    return NextResponse.json(
      { error: "auth check failed", detail: err instanceof Error ? err.message : String(err) },
      { status: 500 },
    );
  }
  return null;
}

async function proxy(req: NextRequest, upstreamPath: string) {
  if (!ADMIN_KEY) {
    return NextResponse.json(
      { error: "SITEPING_ADMIN_KEY not configured" },
      { status: 500 },
    );
  }
  const init: RequestInit = {
    method: req.method,
    headers: {
      "Content-Type": "application/json",
      "X-Siteping-Admin-Key": ADMIN_KEY,
    },
  };
  if (req.method !== "GET" && req.method !== "HEAD" && req.method !== "DELETE") {
    init.body = await req.text();
  }
  try {
    const resp = await fetch(`${BRIDGE_URL}${upstreamPath}`, init);
    const body = await resp.text();
    return new NextResponse(body, {
      status: resp.status,
      headers: { "Content-Type": "application/json" },
    });
  } catch (err) {
    return NextResponse.json(
      { error: "bridge unreachable", detail: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}

export async function GET(req: NextRequest) {
  const unauth = await assertMulticaSession(req);
  if (unauth) return unauth;
  return proxy(req, `/admin/tokens${req.nextUrl.search}`);
}

export async function POST(req: NextRequest) {
  const unauth = await assertMulticaSession(req);
  if (unauth) return unauth;
  return proxy(req, "/admin/tokens");
}
