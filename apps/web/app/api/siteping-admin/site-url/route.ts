/**
 * Server-side proxy for per-project metadata on the SitePing bridge.
 * Uses ?projectId=... in the query (a dynamic [projectId] folder under
 * /api/siteping-admin/ was not picked up by Next.js 16 standalone mode —
 * the request fell through to the backend rewrite. The flat layout side-
 * steps that quirk.)
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

function projectIdFromQuery(req: NextRequest): string | null {
  const pid = req.nextUrl.searchParams.get("projectId");
  return pid && /^[0-9a-f-]{36}$/i.test(pid) ? pid : null;
}

async function forward(req: NextRequest, projectId: string) {
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
  if (req.method === "PUT" || req.method === "POST") {
    init.body = await req.text();
  }
  try {
    const resp = await fetch(
      `${BRIDGE_URL}/admin/project-meta/${encodeURIComponent(projectId)}`,
      init,
    );
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
  const pid = projectIdFromQuery(req);
  if (!pid) return NextResponse.json({ error: "projectId query required" }, { status: 400 });
  return forward(req, pid);
}

export async function PUT(req: NextRequest) {
  const unauth = await assertMulticaSession(req);
  if (unauth) return unauth;
  const pid = projectIdFromQuery(req);
  if (!pid) return NextResponse.json({ error: "projectId query required" }, { status: 400 });
  return forward(req, pid);
}
