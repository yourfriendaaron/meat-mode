// End to end behind a reverse proxy: a real hub with its login on, a Caddy
// container in front of it, and the app in a browser that only ever talks to
// Caddy. The hub hears the proxy's name in Host and knows it only from
// MEAT_MODE_ALLOWED_HOSTS. The tests share one browser context, so the
// session cookie carries from one test to the next, and run in order.
//
// Simulated: the push subscription. A real one needs Chromium to reach a
// browser push service over the internet, so the test posts the JSON a
// browser would send, with an endpoint on a known push host, and reads the
// hub's database to see whose it is. Nothing is pushed.

import { test as base, expect, type BrowserContext, type Page } from "@playwright/test";
import { createHash } from "node:crypto";
import { request as httpRequest } from "node:http";
import { DatabaseSync } from "node:sqlite";
import { CADDY_IMAGE, hasDocker, World } from "./world.ts";

const PASSWORD = "water the plants daily";
const PROXY_HOST = "meat-mode.localhost";
const FAKE_ENDPOINT = "https://fcm.googleapis.com/fcm/send/e2e-proxy-device";

base.describe.configure({ mode: "serial" });
base.skip(!hasDocker(), `docker is not available: the proxy tests run Caddy (${CADDY_IMAGE}) in a container`);

let world: World | undefined;
let ctx: BrowserContext | undefined;
let page: Page | undefined;
let failed = false;

const test = base.extend<{ w: World; app: Page }>({
  w: async ({}, use) => {
    if (!world) throw new Error("world not set up");
    await use(world);
  },
  app: async ({}, use) => {
    if (!page) throw new Error("page not set up");
    await use(page);
  },
});

base.beforeAll(async ({ browser }) => {
  world = await World.create({ password: PASSWORD, proxyHost: PROXY_HOST });
  ctx = await browser.newContext();
  page = await ctx.newPage();
});

base.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) failed = true;
});

base.afterAll(async () => {
  if (failed && world) console.log(world.logs());
  await ctx?.close();
  await world?.dispose();
  world = undefined;
});

// ---------- helpers ----------

interface Reply {
  status: number;
  body: string;
}

/**
 * One request to Caddy on loopback with the headers given, Host included.
 * Node's fetch would pick Host itself, and these tests need to choose it.
 */
function viaProxy(w: World, method: string, path: string, headers: Record<string, string> = {}, body?: string): Promise<Reply> {
  return new Promise((res, rej) => {
    const req = httpRequest(
      { host: "127.0.0.1", port: w.proxyPort, method, path, headers: { host: `${PROXY_HOST}:${w.proxyPort}`, ...headers } },
      (r) => {
        let data = "";
        r.on("data", (d) => (data += d));
        r.on("end", () => res({ status: r.statusCode ?? 0, body: data }));
      },
    );
    req.on("error", rej);
    // An event stream never ends; its status is all these tests need.
    req.setTimeout(5_000, () => req.destroy(new Error(`${method} ${path} timed out`)));
    req.end(body);
  });
}

/** Status of a request from the logged-in page, cookie and all. */
async function fromApp(app: Page, method: string, path: string, body?: unknown): Promise<{ status: number; json: unknown }> {
  return app.evaluate(
    async ({ method, path, body }) => {
      const r = await fetch(path, {
        method,
        headers: body === undefined ? undefined : { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const text = await r.text();
      return { status: r.status, json: text ? JSON.parse(text) : null };
    },
    { method, path, body },
  );
}

async function sessionCookie(): Promise<string> {
  const c = (await ctx!.cookies()).find((c) => c.name === "meat_mode_session");
  if (!c) throw new Error("no session cookie");
  return c.value;
}

/** The session id each push subscription to endpoint belongs to. */
async function pushOwners(w: World, endpoint: string): Promise<string[]> {
  const db = new DatabaseSync(await w.hubDB(), { readOnly: true });
  try {
    const rows = db.prepare("SELECT session_id FROM push_subscriptions WHERE endpoint = ?").all(endpoint) as { session_id: string }[];
    return rows.map((r) => r.session_id);
  } finally {
    db.close();
  }
}

// ---------- tests ----------

test("the app behind Caddy asks for the password, refuses a wrong one and logs in", async ({ w, app }) => {
  await app.goto(w.appURL + "/");
  const password = app.getByLabel("Password");
  await expect(password).toBeVisible();

  await password.fill("water the plant");
  await app.getByRole("button", { name: "Log in" }).click();
  await expect(app.getByRole("alert")).toHaveText("Wrong password.");

  await password.fill(PASSWORD);
  await password.press("Enter");
  await expect(app.locator(".sidebar")).toBeVisible();

  const cookie = (await ctx!.cookies()).find((c) => c.name === "meat_mode_session");
  expect(cookie).toMatchObject({ domain: PROXY_HOST, path: "/", httpOnly: true, sameSite: "Lax", secure: false });
});

test("an item an agent adds through the proxy appears live over the event stream", async ({ w, app }) => {
  const added = await fromApp(app, "POST", "/api/bridges", { name: "agents" });
  expect(added.status).toBe(201);
  const { token } = added.json as { token: string };

  await app.getByRole("navigation", { name: "Views" }).first().getByRole("button", { name: /Everything/ }).click();
  const title = "Return the library books";
  const row = app.locator("#list .row", { has: app.locator(".row-title", { hasText: title }) });
  await expect(row).toHaveCount(0);

  const post = await viaProxy(
    w,
    "POST",
    "/bridge/v1/personal",
    { authorization: `Bearer ${token}`, "content-type": "application/json" },
    JSON.stringify({ title, author: "planner" }),
  );
  expect(post.status).toBe(201);
  // No reload: only the event stream through Caddy can bring it in.
  await expect(row).toBeVisible();
});

test("through the proxy the hub checks Host, Origin and the session; bridges and /healthz pass", async ({ w }) => {
  const json = { "content-type": "application/json" };

  // The proxy's name is accepted; without a cookie only the session routes answer.
  const session = await viaProxy(w, "GET", "/api/session");
  expect(session.status).toBe(200);
  expect(JSON.parse(session.body)).toEqual({ login: true, authenticated: false });
  expect((await viaProxy(w, "GET", "/api/state")).status).toBe(401);
  expect((await viaProxy(w, "GET", "/api/events")).status).toBe(401);
  expect((await viaProxy(w, "POST", "/api/bridges", json, '{"name":"intruder"}')).status).toBe(401);

  // With the browser's cookie the same request goes through.
  const cookie = `meat_mode_session=${await sessionCookie()}`;
  expect((await viaProxy(w, "GET", "/api/state", { cookie })).status).toBe(200);

  // A name the hub does not know, as DNS rebinding would bring.
  expect((await viaProxy(w, "GET", "/api/state", { host: "rebind.example.com", cookie })).status).toBe(421);
  expect((await viaProxy(w, "GET", "/", { host: "rebind.example.com" })).status).toBe(421);

  // Another site, even with the cookie and even for the login itself.
  const evil = { origin: "https://evil.example.com" };
  expect((await viaProxy(w, "POST", "/api/login", { ...json, ...evil }, JSON.stringify({ password: PASSWORD }))).status).toBe(403);
  expect((await viaProxy(w, "GET", "/api/state", { cookie, ...evil })).status).toBe(403);
  expect((await viaProxy(w, "GET", "/api/state", { cookie, "sec-fetch-site": "cross-site" })).status).toBe(403);
  // The app's own origin through the proxy is fine.
  expect((await viaProxy(w, "GET", "/api/state", { cookie, origin: w.appURL })).status).toBe(200);

  // Bridges use their bearer tokens, not the login; /healthz is public.
  expect((await viaProxy(w, "GET", "/healthz")).status).toBe(200);
  expect((await viaProxy(w, "GET", "/bridge/v1/actions")).status).toBe(401);
  const bridge = await viaProxy(w, "POST", "/api/bridges", { ...json, cookie }, '{"name":"laptop"}');
  expect(bridge.status).toBe(201);
  const { token } = JSON.parse(bridge.body) as { token: string };
  expect((await viaProxy(w, "GET", "/bridge/v1/actions", { authorization: `Bearer ${token}` })).status).toBe(200);
});

test("a push subscription belongs to the session and logging out drops it", async ({ w, app }) => {
  // Simulated subscription (see the top of this file).
  const sub = { endpoint: FAKE_ENDPOINT, keys: { p256dh: "BExampleKeyForTheE2eSuiteOnly", auth: "ExampleAuthKey" } };
  const body = JSON.stringify(sub);
  const json = { "content-type": "application/json" };

  expect((await viaProxy(w, "POST", "/api/push/subscriptions", json, body)).status).toBe(401);
  expect((await fromApp(app, "POST", "/api/push/subscriptions", sub)).status).toBe(204);

  const token = await sessionCookie();
  const sessionID = createHash("sha256").update(token).digest("hex");
  expect(await pushOwners(w, FAKE_ENDPOINT)).toEqual([sessionID]);

  await app.locator(".sidebar").getByRole("button", { name: "Settings" }).click();
  await app.getByRole("button", { name: "Log out" }).click();
  await expect(app.getByLabel("Password")).toBeVisible();

  expect(await pushOwners(w, FAKE_ENDPOINT)).toEqual([]);
  expect((await ctx!.cookies()).find((c) => c.name === "meat_mode_session")).toBeUndefined();
  // The old cookie is dead on the hub, not only gone from the browser.
  expect((await viaProxy(w, "GET", "/api/state", { cookie: `meat_mode_session=${token}` })).status).toBe(401);
});
