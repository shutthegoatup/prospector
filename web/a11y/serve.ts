// Playwright's webServer: builds the server, gives a database of its own one member and
// one organisation, and serves the built app from it on 127.0.0.1. The database is made
// on PROSPECTOR_TEST_PG's server, as the Go tests make theirs, and dropped at the end.
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { base, member, org, port, reader } from "./world.ts";

const server = process.env.PROSPECTOR_TEST_PG;
if (!server) {
  console.error("set PROSPECTOR_TEST_PG to a Postgres server this may create a database on (see CLAUDE.md)");
  process.exit(1);
}
const name = `prospector_a11y_${randomBytes(4).toString("hex")}`;
const sql = (statement: string) => execFileSync("psql", [server, "-qc", statement], { stdio: "inherit" });
const database = new URL(server);
database.pathname = `/${name}`;

const dir = mkdtempSync(join(tmpdir(), "prospector-a11y-"));
const bin = join(dir, "prospector");
// A public address and a mail server, so the pages that need them (a forgotten password,
// single sign-on, the front page's form) are there; nothing listens for the mail.
const env: NodeJS.ProcessEnv = {
  ...process.env,
  DATABASE_URL: database.href,
  PROSPECTOR_URL: base,
  SMTP_ADDR: "127.0.0.1:2525",
  MAIL_FROM: "Prospector <a11y@prospector.example>",
};
const root = new URL("../..", import.meta.url).pathname;
const cli = (args: string[], input?: string) =>
  execFileSync(bin, args, { env, input, stdio: ["pipe", "pipe", "inherit"] })
    .toString()
    .trim();
const cleanUp = () => {
  sql(`drop database if exists ${name} with (force)`);
  rmSync(dir, { recursive: true, force: true });
};

try {
  execFileSync("go", ["build", "-o", bin, "."], { cwd: root, stdio: "inherit" });
  sql(`create database ${name}`);
  env.PROSPECTOR_SECRET_KEYS = cli(["secret-key"]);
  cli(["user", "add", member.email, member.name], member.password + "\n");
  cli(["org", "add", org.slug, org.name, `--owner=${member.email}`]);
  cli(["user", "add", reader.email, reader.name], reader.password + "\n");
  cli(["member", "add", org.slug, reader.email, "member"]);
} catch (err) {
  cleanUp();
  throw err;
}

const serving = spawn(bin, ["serve", `127.0.0.1:${port}`], { env, stdio: "inherit" });
for (const signal of ["SIGINT", "SIGTERM"] as const) process.on(signal, () => serving.kill(signal));
serving.on("exit", (code) => {
  cleanUp();
  process.exit(code ?? 0);
});
