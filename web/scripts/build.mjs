// Builds the frontend into web/static: index.html, hashed /assets, /sw.js,
// /manifest.webmanifest and icons. Run with `pnpm --dir web build`.

import { build } from "esbuild";
import { mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { iconPNG, iconSVG } from "./icons.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = join(root, "static");
const dev = process.argv.includes("--dev");

// Clear the previous build, keeping .gitkeep so the Go embed always has a file.
await mkdir(out, { recursive: true });
for (const name of await readdir(out)) {
  if (name !== ".gitkeep") await rm(join(out, name), { recursive: true, force: true });
}

const common = {
  bundle: true,
  minify: !dev,
  sourcemap: dev ? "inline" : false,
  target: ["es2022", "safari16"],
  legalComments: "none",
  logLevel: "warning",
};

const app = await build({
  ...common,
  entryPoints: {
    app: join(root, "src/main.ts"),
    theme: join(root, "src/theme-boot.ts"),
    styles: join(root, "src/styles.css"),
  },
  outdir: out,
  entryNames: "assets/[name]-[hash]",
  format: "esm",
  metafile: true,
});

await build({
  ...common,
  entryPoints: [join(root, "src/sw.ts")],
  outfile: join(out, "sw.js"),
  format: "iife",
});

const assets = {};
for (const [file, meta] of Object.entries(app.metafile.outputs)) {
  if (!meta.entryPoint) continue;
  const name = basename(meta.entryPoint).replace(/\.(ts|css)$/, "");
  assets[name] = "/" + relative(out, join(process.cwd(), file)).split("\\").join("/");
}

const html = `<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#282A36">
<meta name="color-scheme" content="dark light">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<title>Meat Mode</title>
<link rel="manifest" href="/manifest.webmanifest">
<link rel="icon" href="/icon.svg" type="image/svg+xml">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<link rel="stylesheet" href="${assets["styles"]}">
<script src="${assets["theme-boot"]}"></script>
<script type="module" src="${assets["main"]}"></script>
</head>
<body>
<div id="app" class="app"></div>
<noscript>Meat Mode needs JavaScript.</noscript>
</body>
</html>
`;

const manifest = {
  name: "Meat Mode",
  short_name: "Meat Mode",
  description: "Personal to-dos and the tasks agents raise for you.",
  start_url: "/",
  scope: "/",
  display: "standalone",
  background_color: "#282A36",
  theme_color: "#282A36",
  icons: [
    { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
    { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
    { src: "/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    { src: "/icon.svg", sizes: "any", type: "image/svg+xml" },
  ],
};

await writeFile(join(out, "index.html"), html);
await writeFile(join(out, "manifest.webmanifest"), JSON.stringify(manifest, null, 2) + "\n");
await writeFile(join(out, "icon.svg"), iconSVG());
await writeFile(join(out, "icon-192.png"), iconPNG(192, false));
await writeFile(join(out, "icon-512.png"), iconPNG(512, false));
await writeFile(join(out, "icon-maskable-512.png"), iconPNG(512, true));
await writeFile(join(out, "apple-touch-icon.png"), iconPNG(180, true));

for (const k of ["main", "theme-boot", "styles"]) {
  if (!assets[k]) throw new Error(`build: no output for ${k}`);
}
console.log(`built ${relative(process.cwd(), out) || "."}: ${Object.values(assets).join(", ")}, /sw.js`);
