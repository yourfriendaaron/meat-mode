// Minimal static server for web/static, used by Playwright's webServer.
// Mirrors the hub: 404 for missing /assets, index.html for app routes.
// /api is never reached in tests (page.route answers it); it returns 404 here,
// except GET /api/export: the browser fetches a download link itself, past
// page.route, so the export the Settings tests save comes from here. A cookie
// e2e-export=unnamed leaves out its file name.

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
  if (req.method === "GET" && path === "/api/export") {
    const named = !/(^|;\s*)e2e-export=unnamed(;|$)/.test(req.headers.cookie ?? "");
    res.writeHead(200, {
      "content-type": "application/zip",
      ...(named ? { "content-disposition": 'attachment; filename="meat-mode-export-2026-10-14.zip"' } : {}),
    });
    res.end("PK\x03\x04 an export");
    return;
  }
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
