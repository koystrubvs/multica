/**
 * Server-side proxy for DELETE on a single SitePing token. Gated by the
 * same Multica session check as /api/siteping-admin/tokens.
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

export async function DELETE(
  req: NextRequest,
  ctx: { params: Promise<{ tokenId: string }> },
) {
  const unauth = await assertMulticaSession(req);
  if (unauth) return unauth;

  if (!ADMIN_KEY) {
    return NextResponse.json(
      { error: "SITEPING_ADMIN_KEY not configured" },
      { status: 500 },
    );
  }
  const { tokenId } = await ctx.params;
  try {
    const resp = await fetch(`${BRIDGE_URL}/admin/tokens/${encodeURIComponent(tokenId)}`, {
      method: "DELETE",
      headers: { "X-Siteping-Admin-Key": ADMIN_KEY },
    });
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
