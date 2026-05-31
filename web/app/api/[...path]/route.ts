import type { NextRequest } from "next/server";
import { proxyApiRequest } from "@/lib/backend-proxy";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

async function handler(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyApiRequest(request, path);
}

export const DELETE = handler;
export const GET = handler;
export const HEAD = handler;
export const OPTIONS = handler;
export const PATCH = handler;
export const POST = handler;
export const PUT = handler;
