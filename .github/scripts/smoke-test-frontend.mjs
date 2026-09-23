import assert from "node:assert/strict";
import { once } from "node:events";
import { createServer } from "node:http";
import { setTimeout } from "node:timers/promises";

// Run through docker exec as the image's default user. The container has no
// external network; this loopback fixture is the only backend it can reach.
assert.notEqual(process.getuid(), 0, "frontend must run as a non-root user");
const backend = createServer(async (request, response) => {
  let body = "";
  for await (const chunk of request) body += chunk;
  response.writeHead(request.method === "POST" ? 201 : 200, {
    "content-type": "application/json",
    "x-sparkle-ci": "fixture"
  });
  response.end(JSON.stringify({ method: request.method, path: request.url, body }));
});
backend.listen(4321, "127.0.0.1");
await once(backend, "listening");

const origin = "http://127.0.0.1:3000";
const request = (path, options = {}) => fetch(new URL(path, origin), {
  ...options,
  signal: AbortSignal.timeout(5000)
});

try {
  let page;
  for (let attempt = 0; attempt < 30; attempt++) {
    try {
      page = await request("/");
      if (page.ok) break;
      await page.body?.cancel();
    } catch {
      // The production server may still be starting.
    }
    await setTimeout(1000);
  }
  assert.ok(page?.ok, "production homepage did not become ready");
  assert.match(page.headers.get("content-type"), /text\/html/);
  const html = await page.text();
  assert.match(html, /Sparkle Transcoder/);

  const script = html.match(/src="([^"\s]+\.js(?:\?[^"\s]*)?)"/);
  assert.ok(script, "homepage must reference a compiled JavaScript bundle");
  const bundle = await request(script[1].replaceAll("&amp;", "&"));
  assert.equal(bundle.status, 200, "compiled JavaScript must be served");
  assert.match(bundle.headers.get("content-type"), /javascript/);
  assert.ok((await bundle.text()).length > 0);
  assert.equal((await request("/icon.svg")).status, 200, "public assets must be served");

  const api = await request("/api/ci-fixture?probe=runtime");
  assert.equal(api.status, 200);
  assert.equal(api.headers.get("x-sparkle-ci"), "fixture");
  assert.deepEqual(await api.json(), {
    method: "GET", path: "/api/ci-fixture?probe=runtime", body: ""
  });

  const body = JSON.stringify({ smoke: true });
  const post = await request("/api/ci-fixture", {
    method: "POST", headers: { "content-type": "application/json" }, body
  });
  assert.equal(post.status, 201, "proxy must preserve backend status codes");
  assert.deepEqual(await post.json(), { method: "POST", path: "/api/ci-fixture", body });

  const output = await request("/output/ci%20fixture.txt?download=1");
  assert.equal(output.status, 200);
  assert.deepEqual(await output.json(), {
    method: "GET", path: "/output/ci%20fixture.txt?download=1", body: ""
  });
  console.log("Smoke test passed: non-root startup, homepage, static assets, runtime API and output proxies.");
} finally {
  backend.closeAllConnections();
  backend.close();
}
