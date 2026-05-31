import type { NextRequest } from "next/server";

const DEFAULT_BACKEND_API_BASE = "http://host.docker.internal:1323/api";

function backendApiBase() {
  return (process.env.SPARKLE_API_BASE || DEFAULT_BACKEND_API_BASE).trim().replace(/\/$/, "");
}

function backendRoot() {
  return backendApiBase().replace(/\/api$/, "");
}

function encodePath(parts: string[]) {
  return parts.map((part) => encodeURIComponent(part)).join("/");
}

function requestHeaders(request: NextRequest) {
  const headers = new Headers(request.headers);
  headers.delete("connection");
  headers.delete("content-length");
  headers.delete("host");
  return headers;
}

function responseHeaders(headers: Headers) {
  const nextHeaders = new Headers(headers);
  nextHeaders.delete("connection");
  nextHeaders.delete("content-encoding");
  nextHeaders.delete("content-length");
  nextHeaders.delete("transfer-encoding");
  return nextHeaders;
}

async function proxyRequest(request: NextRequest, target: string) {
  const hasBody = request.method !== "GET" && request.method !== "HEAD";
  const response = await fetch(target, {
    method: request.method,
    headers: requestHeaders(request),
    body: hasBody ? await request.arrayBuffer() : undefined,
    cache: "no-store",
    redirect: "manual"
  });

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers: responseHeaders(response.headers)
  });
}

export async function proxyApiRequest(request: NextRequest, path: string[]) {
  const target = `${backendApiBase()}/${encodePath(path)}${request.nextUrl.search}`;
  return proxyRequest(request, target);
}

export async function proxyOutputRequest(request: NextRequest, path: string[]) {
  const target = `${backendRoot()}/output/${encodePath(path)}${request.nextUrl.search}`;
  return proxyRequest(request, target);
}
