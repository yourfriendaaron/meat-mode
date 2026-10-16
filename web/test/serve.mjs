// Minimal static server for web/static, used by Playwright's webServer.
// Mirrors the hub: 404 for missing /assets, index.html for app routes.
// /api is never reached in tests (page.route answers it); it returns 404 here.

import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../static/", import.meta.url));
const port = Number(process.env.PORT || 4173);
const types = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".webmanifest": "application/manifest+json",
};

createServer(async (req, res) => {
  const path = normalize(decodeURIComponent(new URL(req.url, "http://x").pathname));
  if (path.startsWith("/api/")) {
    res.writeHead(404, { "content-type": "application/json" }).end('{"error":"not found"}');
    return;
  }
  const file = join(root, path);
  const isAsset = extname(path) !== "";
  try {
    if (!file.startsWith(root)) throw new Error("outside root");
    const body = await readFile(isAsset ? file : join(root, "index.html"));
    res.writeHead(200, {
      "content-type": types[isAsset ? extname(path) : ".html"] ?? "application/octet-stream",
      "content-security-policy": "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'",
    });
    res.end(body);
  } catch {
    res.writeHead(404).end("not found");
  }
}).listen(port, "127.0.0.1", () => console.log(`serving web/static on http://127.0.0.1:${port}`));
