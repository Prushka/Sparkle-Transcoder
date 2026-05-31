import type { NextRequest } from "next/server";
import { proxyOutputRequest } from "@/lib/backend-proxy";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

async function handler(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyOutputRequest(request, path);
}

export const GET = handler;
export const HEAD = handler;
