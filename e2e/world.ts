// The world the end-to-end suite runs in: a real hub, an ntfy capture server,
// and for each bridge a home directory, scratch beads repos and a
// `meat-mode bridge run` child process. Everything lives under one temp
// directory that dispose() removes; an exit handler kills any child a crashed
// run leaves behind.

import { execFile, spawn, spawnSync, type ChildProcess } from "node:child_process";
import { appendFile, mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { createServer, type IncomingHttpHeaders, type Server } from "node:http";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileP = promisify(execFile);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** Poll interval the bridges run with (MEAT_MODE_BRIDGE_POLL). */
export const POLL = "200ms";
export const USER = "alex";
export const NTFY_TOPIC = "meat-mode-e2e";

// ---------- cleanup on any exit ----------

const liveChildren = new Set<ChildProcess>();
const liveContainers = new Set<string>();
const liveVolumes = new Set<string>();
process.on("exit", () => {
  for (const c of liveChildren) {
    try {
      c.kill("SIGKILL");
    } catch {
      /* already gone */
    }
  }
  for (const name of liveContainers) spawnSync("docker", ["rm", "-f", name], { stdio: "ignore" });
  for (const name of liveVolumes) spawnSync("docker", ["volume", "rm", "-f", name], { stdio: "ignore" });
});

/** Whether a docker command (Docker, or Podman behind it) answers. */
export function hasDocker(): boolean {
  const r = spawnSync("docker", ["version"], { stdio: "ignore" });
  return r.status === 0;
}

/** The Caddy image the proxy tests run, pinned. */
export const CADDY_IMAGE = "caddy:2.10.2-alpine";

/** Whether bd is on PATH. */
export function hasBd(): boolean {
  const r = spawnSync("bd", ["version"], { stdio: "ignore" });
  return r.status === 0;
}

async function freePort(): Promise<number> {
  return new Promise((res, rej) => {
    const s = createNetServer();
    s.once("error", rej);
    s.listen(0, "127.0.0.1", () => {
      const addr = s.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      s.close(() => res(port));
    });
  });
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---------- child processes ----------

/** A child process whose output is kept for the failure report. */
export class Proc {
  readonly child: ChildProcess;
  output = "";
  readonly exited: Promise<number | null>;
  readonly name: string;
  private readonly onStop?: () => void;

  constructor(name: string, cmd: string, args: string[], env: NodeJS.ProcessEnv, onStop?: () => void) {
    this.name = name;
    this.onStop = onStop;
    this.child = spawn(cmd, args, { env, stdio: ["ignore", "pipe", "pipe"] });
    liveChildren.add(this.child);
    const keep = (d: Buffer) => {
      this.output += d.toString();
    };
    this.child.stdout!.on("data", keep);
    this.child.stderr!.on("data", keep);
    this.exited = new Promise((res) => {
      this.child.on("exit", (code) => {
        liveChildren.delete(this.child);
        res(code);
      });
      this.child.on("error", (err) => {
        this.output += `spawn error: ${err.message}\n`;
        liveChildren.delete(this.child);
        res(-1);
      });
    });
  }

  get running(): boolean {
    return this.child.exitCode === null && this.child.signalCode === null;
  }

  /** SIGTERM, then SIGKILL after 10 seconds. */
  async stop(): Promise<void> {
    this.onStop?.();
    if (!this.running) return;
    this.child.kill("SIGTERM");
    const killed = await Promise.race([this.exited.then(() => true), sleep(10_000).then(() => false)]);
    if (!killed) {
      this.child.kill("SIGKILL");
      await this.exited;
    }
  }
}

// ---------- ntfy capture ----------

export interface Captured {
  method: string;
  url: string;
  headers: IncomingHttpHeaders;
  body: string;
  at: number; // Date.now() when it arrived
}

/** A tiny HTTP server standing in for ntfy; it records every request. */
class Capture {
  requests: Captured[] = [];
  private server!: Server;
  url = "";

  async start(): Promise<void> {
    this.server = createServer((req, res) => {
      let body = "";
      req.on("data", (d) => (body += d));
      req.on("end", () => {
        this.requests.push({ method: req.method ?? "", url: req.url ?? "", headers: req.headers, body, at: Date.now() });
        res.writeHead(200, { "content-type": "application/json" }).end("{}");
      });
    });
    await new Promise<void>((res) => this.server.listen(0, "127.0.0.1", () => res()));
    const addr = this.server.address();
    this.url = `http://127.0.0.1:${typeof addr === "object" && addr ? addr.port : 0}`;
  }

  async stop(): Promise<void> {
    this.server.closeAllConnections();
    await new Promise<void>((res) => this.server.close(() => res()));
  }
}

// ---------- beads ----------

export interface Issue {
  id: string;
  title: string;
  status: string;
  close_reason?: string;
  labels?: string[];
  due_at?: string;
  defer_until?: string;
  updated_at?: string;
  comment_count?: number;
  metadata?: Record<string, unknown>;
  comments?: { author: string; text: string }[];
}

export interface BeadEvent {
  issue_id: string;
  event_type: string;
  actor: string;
}

/** One paired agent machine and its scratch repos. */
export interface Machine {
  name: string;
  home: string;
  root: string;
  /** The bridge token from Settings, once paired. */
  token?: string;
  /** Repo name (as the bridge reports it) to directory. */
  repos: Record<string, string>;
  proc?: Proc;
}

export interface RepoSpec {
  name: string; // directory name under the machine's projects folder
  prefix: string;
  optedIn: boolean;
  /** Titles of beads labelled human. */
  human: string[];
  /** Titles of beads without the human label. */
  other?: string[];
}

/** How World.create sets up the hub. The default is no login and no proxy. */
export interface WorldOptions {
  /** Turns the hub's login on with this password. */
  password?: string;
  /**
   * Puts a Caddy container in front of the hub and serves the app at
   * http://<proxyHost>:<port>, a name the hub only knows from
   * MEAT_MODE_ALLOWED_HOSTS. Use a *.localhost name: Chromium sends it to
   * loopback and treats it as a secure context.
   */
  proxyHost?: string;
}

export class World {
  tmp = "";
  bin = "";
  /** The hub itself, on loopback. */
  hubURL = "";
  /** Where the browser opens the app: the proxy when there is one, else the hub. */
  appURL = "";
  /** The proxy's port on loopback, or 0. */
  proxyPort = 0;
  hub!: Proc;
  proxy?: Proc;
  ntfy = new Capture();
  machines = new Map<string, Machine>();
  /** Bead title to id, for every seeded or added bead. */
  ids = new Map<string, string>();
  private bdHome = "";
  private hubPort = 0;
  private hubVolume = "";
  private hubContainer = "";
  private opts: WorldOptions = {};
  private passwordHash = "";

  /** Builds the binary, starts the capture server, the hub and any proxy. */
  static async create(opts: WorldOptions = {}): Promise<World> {
    const w = new World();
    w.opts = opts;
    w.tmp = await mkdtemp(join(tmpdir(), "meat-mode-e2e-"));
    w.bdHome = join(w.tmp, "bd-home");
    await mkdir(w.bdHome, { recursive: true });
    try {
      await w.build();
      await w.ntfy.start();
      if (opts.password !== undefined) w.passwordHash = w.hashPassword(opts.password);
      if (opts.proxyHost) w.proxyPort = await freePort();
      await w.startHub();
      if (opts.proxyHost) await w.startProxy(opts.proxyHost);
      else w.appURL = w.hubURL;
    } catch (e) {
      await w.dispose();
      throw e;
    }
    return w;
  }

  private async build(): Promise<void> {
    if (process.env.E2E_BIN) {
      this.bin = resolve(process.env.E2E_BIN);
      return;
    }
    try {
      await stat(join(repoRoot, "web", "static", "index.html"));
    } catch {
      throw new Error("web/static is not built; run `pnpm --dir web build` first (the e2e `test` script does)");
    }
    this.bin = join(this.tmp, "meat-mode");
    await execFileP("go", ["build", "-o", this.bin, "./cmd/meat-mode"], { cwd: repoRoot, env: { ...process.env, CGO_ENABLED: "0" } });
  }

  /** Runs `meat-mode hash-password` with the password on stdin, as an operator would. */
  private hashPassword(password: string): string {
    const r = spawnSync(this.bin, ["hash-password"], { input: password + "\n", encoding: "utf8" });
    if (r.status !== 0) throw new Error(`hash-password failed: ${r.stderr}`);
    return r.stdout.trim();
  }

  /**
   * Starts Caddy in a container in front of the hub. Host networking lets it
   * listen on the free port and reach the hub on loopback, with Docker and
   * rootless Podman alike. The site matches any Host, so the hub's own Host
   * check is what refuses a wrong name.
   */
  private async startProxy(host: string): Promise<void> {
    const port = this.proxyPort;
    const caddyfile = join(this.tmp, "Caddyfile");
    await writeFile(caddyfile, `{\n\tadmin off\n}\n\nhttp://:${port} {\n\treverse_proxy 127.0.0.1:${this.hubPort}\n}\n`);
    const name = `meat-mode-e2e-caddy-${port}`;
    liveContainers.add(name);
    this.proxy = new Proc(
      "caddy",
      "docker",
      ["run", "--rm", "--name", name, "--network", "host", "-v", `${caddyfile}:/etc/caddy/Caddyfile:ro`, CADDY_IMAGE],
      process.env,
      () => {
        spawnSync("docker", ["rm", "-f", name], { stdio: "ignore" });
        liveContainers.delete(name);
      },
    );
    this.appURL = `http://${host}:${port}`;
    const deadline = Date.now() + 60_000;
    for (;;) {
      try {
        const r = await fetch(`http://127.0.0.1:${port}/healthz`);
        if (r.ok) return;
      } catch {
        /* not up yet */
      }
      if (!this.proxy.running || Date.now() > deadline) throw new Error(`caddy did not start:\n${this.proxy.output}`);
      await sleep(200);
    }
  }

  /**
   * A local path to the hub's database, readable while the hub runs. For a
   * hub in a container it is a fresh copy of the data directory.
   */
  async hubDB(): Promise<string> {
    if (!this.hubContainer) return join(this.tmp, "hub-data", "meat-mode.db");
    const dest = join(this.tmp, `hub-data-copy-${Date.now()}`);
    const r = spawnSync("docker", ["cp", `${this.hubContainer}:/data`, dest], { encoding: "utf8" });
    if (r.status !== 0) throw new Error(`docker cp failed: ${r.stderr}`);
    return join(dest, "meat-mode.db");
  }

  /**
   * A file in the hub's data directory, or null when it does not exist. For
   * a hub in a container it is copied out first; the image has no shell.
   */
  async hubFile(name: string): Promise<string | null> {
    let path = join(this.tmp, "hub-data", name);
    if (this.hubContainer) {
      path = join(this.tmp, `hub-file-${Date.now()}`);
      const r = spawnSync("docker", ["cp", `${this.hubContainer}:/data/${name}`, path], { encoding: "utf8" });
      if (r.status !== 0) return null;
    }
    try {
      return await readFile(path, "utf8");
    } catch {
      return null;
    }
  }

  /** Stops the hub and starts it again on the same address and data. */
  async restartHub(): Promise<void> {
    await this.hub.stop();
    await this.startHub();
  }

  private async startHub(): Promise<void> {
    const port = (this.hubPort ||= await freePort());
    this.hubURL = `http://127.0.0.1:${port}`;
    const env: Record<string, string> = {
      MEAT_MODE_LISTEN: `127.0.0.1:${port}`,
      MEAT_MODE_PUBLIC_URL: this.hubURL,
      MEAT_MODE_USER: USER,
      MEAT_MODE_VAPID_SUBJECT: "mailto:alex@example.com",
      MEAT_MODE_NTFY_URL: this.ntfy.url,
      MEAT_MODE_NTFY_TOPIC: NTFY_TOPIC,
      MEAT_MODE_REMINDER_INTERVAL: "1s",
    };
    if (this.passwordHash) env.MEAT_MODE_PASSWORD_HASH = this.passwordHash;
    if (this.opts.proxyHost) {
      // The address the user opens is the proxy's; the hub hears its name in Host.
      env.MEAT_MODE_PUBLIC_URL = `http://localhost:${this.proxyPort}`;
      env.MEAT_MODE_ALLOWED_HOSTS = this.opts.proxyHost;
    }
    const image = process.env.E2E_IMAGE;
    if (image) {
      // Host networking lets the container listen on the free port and reach
      // the capture server on loopback, with Docker and rootless Podman alike.
      const name = `meat-mode-e2e-${port}`;
      this.hubContainer = name;
      // A named volume keeps the data across restartHub, as a deployment does.
      this.hubVolume = `${name}-data`;
      liveContainers.add(name);
      liveVolumes.add(this.hubVolume);
      const args = ["run", "--rm", "--name", name, "--network", "host", "-v", `${this.hubVolume}:/data`];
      for (const [k, v] of Object.entries(env)) args.push("-e", `${k}=${v}`);
      args.push(image);
      this.hub = new Proc("hub", "docker", args, process.env, () => {
        spawnSync("docker", ["rm", "-f", name], { stdio: "ignore" });
        liveContainers.delete(name);
      });
    } else {
      const data = join(this.tmp, "hub-data");
      await mkdir(data, { recursive: true });
      this.hub = new Proc("hub", this.bin, ["hub"], { ...process.env, ...env, MEAT_MODE_DATA_DIR: data });
    }
    const deadline = Date.now() + 60_000;
    for (;;) {
      try {
        const r = await fetch(`${this.hubURL}/healthz`);
        if (r.ok) return;
      } catch {
        /* not up yet */
      }
      if (!this.hub.running || Date.now() > deadline) throw new Error(`hub did not start:\n${this.hub.output}`);
      await sleep(200);
    }
  }

  /** Environment for bd calls the tests make. */
  private bdEnv(home = this.bdHome): NodeJS.ProcessEnv {
    return {
      ...process.env,
      HOME: home,
      XDG_CONFIG_HOME: join(home, ".config"),
      XDG_STATE_HOME: join(home, ".local", "state"),
      BD_NON_INTERACTIVE: "1",
      NO_COLOR: "1",
      BEADS_ACTOR: USER,
      GIT_AUTHOR_NAME: "Alex",
      GIT_AUTHOR_EMAIL: "alex@example.com",
      GIT_COMMITTER_NAME: "Alex",
      GIT_COMMITTER_EMAIL: "alex@example.com",
      GIT_CONFIG_NOSYSTEM: "1",
    };
  }

  async bd(dir: string, ...args: string[]): Promise<string> {
    // bd init has no project to change into yet, so it runs in dir instead.
    const full = args[0] === "init" ? args : ["-C", dir, ...args];
    try {
      const { stdout } = await execFileP("bd", full, { cwd: dir, env: this.bdEnv(), maxBuffer: 16 << 20 });
      return stdout;
    } catch (e) {
      const err = e as { stdout?: string; stderr?: string; message: string };
      throw new Error(`bd ${args.join(" ")} in ${dir}: ${err.stderr || err.stdout || err.message}`);
    }
  }

  async bdJSON<T>(dir: string, ...args: string[]): Promise<T> {
    const out = await this.bd(dir, ...args, "--json");
    const i = out.search(/[[{]/);
    return JSON.parse(out.slice(i)) as T;
  }

  async show(dir: string, id: string): Promise<Issue> {
    const list = await this.bdJSON<Issue[]>(dir, "show", id, "--include-comments");
    return list[0];
  }

  async events(dir: string, id: string): Promise<BeadEvent[]> {
    return this.bdJSON<BeadEvent[]>(dir, "history", id, "--events");
  }

  async list(dir: string): Promise<Issue[]> {
    return this.bdJSON<Issue[]>(dir, "list", "--all", "-n", "0");
  }

  async create(dir: string, title: string, ...extra: string[]): Promise<string> {
    const issue = await this.bdJSON<Issue>(dir, "create", `--title=${title}`, ...extra);
    this.ids.set(title, issue.id);
    return issue.id;
  }

  /** Makes a machine's home, its project root and its scratch repos. */
  async addMachine(name: string, specs: RepoSpec[]): Promise<Machine> {
    const base = join(this.tmp, name);
    const home = join(base, "home");
    const m: Machine = { name, home, root: join(home, "projects"), repos: {} };
    await mkdir(m.home, { recursive: true });
    await mkdir(m.root, { recursive: true });
    for (const s of specs) {
      const dir = join(m.root, s.name);
      await mkdir(dir, { recursive: true });
      await execFileP("git", ["init", "-q"], { cwd: dir, env: this.bdEnv() });
      await this.bd(dir, "init", "-q", `--prefix=${s.prefix}`, "--non-interactive", "--skip-agents", "--skip-hooks");
      if (s.optedIn) await appendFile(join(dir, ".beads", "config.yaml"), "\nmeat-mode.sync: true\n");
      for (const t of s.human) await this.create(dir, t, "-l", "human");
      for (const t of s.other ?? []) await this.create(dir, t);
      m.repos[s.name] = dir;
    }
    this.machines.set(name, m);
    return m;
  }

  private bridgeEnv(m: Machine): NodeJS.ProcessEnv {
    return { ...this.bdEnv(m.home), MEAT_MODE_BRIDGE_POLL: POLL };
  }

  /** Runs `meat-mode bridge pair` for a machine with the token from the app. */
  async pair(name: string, url: string, token: string): Promise<string> {
    const m = this.machine(name);
    const { stdout } = await execFileP(this.bin, ["bridge", "pair", url, token, "--no-service"], { env: this.bridgeEnv(m) });
    m.token = token;
    return stdout;
  }

  /** Runs a meat-mode command on a paired machine, as its agents do. extra adds to its environment. */
  async run(name: string, args: string[], extra: NodeJS.ProcessEnv = {}): Promise<string> {
    const env = { ...this.bridgeEnv(this.machine(name)), ...extra };
    const { stdout } = await execFileP(this.bin, args, { env });
    return stdout;
  }

  machine(name: string): Machine {
    const m = this.machines.get(name);
    if (!m) throw new Error(`no machine ${name}`);
    return m;
  }

  startBridge(name: string): Proc {
    const m = this.machine(name);
    if (m.proc?.running) throw new Error(`bridge ${name} is already running`);
    const prev = m.proc?.output ?? "";
    m.proc = new Proc(`bridge ${name}`, this.bin, ["bridge", "run"], this.bridgeEnv(m));
    m.proc.output = prev;
    return m.proc;
  }

  async stopBridge(name: string): Promise<void> {
    await this.machine(name).proc?.stop();
  }

  async api<T>(path: string, init?: RequestInit): Promise<T> {
    const r = await fetch(this.hubURL + path, init);
    if (!r.ok) throw new Error(`${init?.method ?? "GET"} ${path}: ${r.status} ${await r.text()}`);
    return (r.status === 204 ? undefined : await r.json()) as T;
  }

  /** The tail of every process's output, for a failure report. */
  logs(): string {
    const procs = [this.hub, this.proxy, ...[...this.machines.values()].map((m) => m.proc)].filter((p): p is Proc => !!p);
    return procs.map((p) => `----- ${p.name} -----\n${p.output.slice(-8000)}`).join("\n");
  }

  async dispose(): Promise<void> {
    for (const m of this.machines.values()) await m.proc?.stop();
    await this.proxy?.stop();
    await this.hub?.stop();
    if (this.hubVolume) {
      spawnSync("docker", ["volume", "rm", "-f", this.hubVolume], { stdio: "ignore" });
      liveVolumes.delete(this.hubVolume);
    }
    await this.ntfy.stop().catch(() => {});
    if (this.tmp) await rm(this.tmp, { recursive: true, force: true });
  }
}
