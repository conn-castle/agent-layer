import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { loadEnvFile, applyEnv } from "../src/lib/env.mjs";

describe("src/lib/env.mjs", () => {
  let tmpDir;

  before(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "agent-layer-tests-env-"));
  });

  after(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  test("loadEnvFile returns empty object if file missing", () => {
    const res = loadEnvFile(path.join(tmpDir, "missing.env"));
    assert.strictEqual(res.loaded, false);
    assert.deepStrictEqual(res.env, {});
  });

  test("loadEnvFile parses basic key-value pairs", () => {
    const p = path.join(tmpDir, "basic.env");
    fs.writeFileSync(p, "KEY=value\nANOTHER=123\n");
    const res = loadEnvFile(p);
    assert.strictEqual(res.loaded, true);
    assert.deepStrictEqual(res.env, { KEY: "value", ANOTHER: "123" });
  });

  test("loadEnvFile ignores comments and empty lines", () => {
    const p = path.join(tmpDir, "comments.env");
    fs.writeFileSync(p, "\n# This is a comment\nKEY=value\n  \n");
    const res = loadEnvFile(p);
    assert.deepStrictEqual(res.env, { KEY: "value" });
  });

  test("loadEnvFile handles 'export' prefix", () => {
    const p = path.join(tmpDir, "export.env");
    fs.writeFileSync(p, "export KEY=value\n");
    const res = loadEnvFile(p);
    assert.deepStrictEqual(res.env, { KEY: "value" });
  });

  test("loadEnvFile handles quotes", () => {
    const p = path.join(tmpDir, "quotes.env");
    fs.writeFileSync(p, "KEY=\"value with spaces\"\nSINGLE='single quotes'\n");
    const res = loadEnvFile(p);
    assert.deepStrictEqual(res.env, {
      KEY: "value with spaces",
      SINGLE: "single quotes",
    });
  });

  test("loadEnvFile throws on invalid lines", () => {
    const p = path.join(tmpDir, "invalid.env");
    fs.writeFileSync(p, "INVALID LINE\n");
    assert.throws(() => loadEnvFile(p), /Invalid env entry/);
  });

  test("loadEnvFile throws on duplicate keys", () => {
    const p = path.join(tmpDir, "dup.env");
    fs.writeFileSync(p, "KEY=val1\nKEY=val2\n");
    assert.throws(() => loadEnvFile(p), /Duplicate env entry/);
  });

  test("loadEnvFile throws on unmatched quotes", () => {
    const p = path.join(tmpDir, "unmatched.env");
    fs.writeFileSync(p, 'KEY="value\n');
    assert.throws(() => loadEnvFile(p), /Remove unmatched quotes/);
  });

  test("applyEnv merges environments", () => {
    const base = { A: "1" };
    const add = { B: "2" };
    const res = applyEnv(base, add);
    assert.deepStrictEqual(res, { A: "1", B: "2" });
  });
});
