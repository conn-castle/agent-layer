import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import {
  stripJsoncComments,
  removeTrailingCommas,
  readJsonRelaxed,
} from "../src/sync/utils.mjs";

describe("src/sync/utils.mjs", () => {
  let tmpDir;

  before(() => {
    tmpDir = fs.mkdtempSync(
      path.join(os.tmpdir(), "agent-layer-tests-sync-utils-"),
    );
  });

  after(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  test("stripJsoncComments removes line comments", () => {
    const input = '{\n  // line comment\n  "a": 1\n}';
    const result = stripJsoncComments(input);
    assert.ok(!result.includes("//"));
    assert.ok(result.includes('"a": 1'));
  });

  test("stripJsoncComments removes block comments", () => {
    const input = '{\n  "a": 1 /* block comment */\n}';
    const result = stripJsoncComments(input);
    assert.ok(!result.includes("/*"));
    assert.ok(!result.includes("*/"));
    assert.ok(result.includes('"a": 1'));
  });

  test("removeTrailingCommas handles arrays", () => {
    const input = '{"b": [1, 2,]}';
    const result = removeTrailingCommas(input);
    const parsed = JSON.parse(result);
    assert.deepStrictEqual(parsed.b, [1, 2]);
  });

  test("removeTrailingCommas handles objects", () => {
    const input = '{"c": {"d": "ok",}}';
    const result = removeTrailingCommas(input);
    const parsed = JSON.parse(result);
    assert.strictEqual(parsed.c.d, "ok");
  });

  test("stripJsoncComments and removeTrailingCommas work together", () => {
    const input = `{
  // line comment
  "a": 1, /* block comment */
  "b": [1, 2,],
  "c": { "d": "ok", },
}`;
    const stripped = stripJsoncComments(input);
    const cleaned = removeTrailingCommas(stripped);
    const parsed = JSON.parse(cleaned);

    assert.strictEqual(parsed.a, 1);
    assert.deepStrictEqual(parsed.b, [1, 2]);
    assert.strictEqual(parsed.c.d, "ok");
  });

  test("readJsonRelaxed returns fallback for missing file", () => {
    const missingPath = path.join(tmpDir, "missing.json");
    const result = readJsonRelaxed(missingPath, { default: true });
    assert.deepStrictEqual(result, { default: true });
  });

  test("readJsonRelaxed parses JSONC with comments and trailing commas", () => {
    const filePath = path.join(tmpDir, "relaxed.jsonc");
    fs.writeFileSync(
      filePath,
      `{
  "answer": 42,
  // comment
  "values": [1, 2,],
}`,
    );

    const parsed = readJsonRelaxed(filePath, null);
    assert.strictEqual(parsed.answer, 42);
    assert.deepStrictEqual(parsed.values, [1, 2]);
  });

  test("readJsonRelaxed throws on invalid JSON with helpful error", () => {
    const filePath = path.join(tmpDir, "invalid.json");
    fs.writeFileSync(
      filePath,
      `{
  "answer":
}`,
    );

    assert.throws(() => {
      readJsonRelaxed(filePath, null);
    }, /Unexpected token/);
  });
});
