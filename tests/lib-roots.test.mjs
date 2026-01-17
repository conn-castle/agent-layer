import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { resolveParentRoot } from "../src/lib/roots.mjs";

describe("src/lib/roots.mjs", () => {
  let tmpBase;

  before(() => {
    tmpBase = fs.mkdtempSync(
      path.join(os.tmpdir(), "agent-layer-tests-roots-"),
    );
  });

  after(() => {
    fs.rmSync(tmpBase, { recursive: true, force: true });
  });

  test("resolveParentRoot throws on conflicting flags", () => {
    assert.throws(() => {
      resolveParentRoot({
        parentRoot: "/some/path",
        useTempParentRoot: true,
        agentLayerRoot: tmpBase,
      });
    }, /Conflicting flags/);
  });

  test("resolveParentRoot throws if explicit parent root missing", () => {
    const missing = path.join(tmpBase, "missing");
    assert.throws(() => {
      resolveParentRoot({
        parentRoot: missing,
        agentLayerRoot: tmpBase,
      });
    }, /Parent root path does not exist/);
  });

  test("resolveParentRoot throws if explicit parent root missing .agent-layer", () => {
    const emptyParent = path.join(tmpBase, "empty-parent");
    fs.mkdirSync(emptyParent);
    assert.throws(() => {
      resolveParentRoot({
        parentRoot: emptyParent,
        agentLayerRoot: tmpBase,
      });
    }, /Parent root must contain .agent-layer/);
  });

  test("resolveParentRoot creates temp parent root", () => {
    const agentLayerRoot = path.join(tmpBase, "real-agent-layer");
    fs.mkdirSync(agentLayerRoot);

    const result = resolveParentRoot({
      useTempParentRoot: true,
      agentLayerRoot: agentLayerRoot,
    });

    assert.ok(result.tempParentRootCreated);
    assert.ok(fs.existsSync(result.parentRoot));
    const linkPath = path.join(result.parentRoot, ".agent-layer");
    assert.ok(fs.lstatSync(linkPath).isSymbolicLink());
    assert.strictEqual(
      fs.realpathSync(linkPath),
      fs.realpathSync(agentLayerRoot),
    );

    // Cleanup
    result.cleanupTempParentRoot();
    assert.ok(!fs.existsSync(result.parentRoot));
  });

  test("resolveParentRoot auto-discovers if .agent-layer", () => {
    // Setup: /tmp/my-repo/.agent-layer
    const repo = path.join(tmpBase, "my-repo");
    const al = path.join(repo, ".agent-layer");
    fs.mkdirSync(al, { recursive: true });

    // When agentLayerRoot is .../.agent-layer
    const result = resolveParentRoot({
      agentLayerRoot: al,
    });

    assert.strictEqual(result.parentRoot, fs.realpathSync(repo));
    assert.strictEqual(result.isConsumerLayout, true);
  });

  test("resolveParentRoot uses PARENT_ROOT from .env in dev repo", () => {
    const agentLayerRoot = path.join(tmpBase, "agent-layer-dev");
    const parentRoot = path.join(tmpBase, "consumer-root");
    fs.mkdirSync(agentLayerRoot, { recursive: true });
    fs.mkdirSync(parentRoot, { recursive: true });
    fs.symlinkSync(agentLayerRoot, path.join(parentRoot, ".agent-layer"));
    fs.writeFileSync(
      path.join(agentLayerRoot, ".env"),
      `PARENT_ROOT=${parentRoot}\n`,
    );

    const result = resolveParentRoot({
      agentLayerRoot: agentLayerRoot,
    });

    assert.strictEqual(result.parentRoot, fs.realpathSync(parentRoot));
    assert.strictEqual(result.isConsumerLayout, false);
  });

  test("resolveParentRoot errors when PARENT_ROOT in .env is invalid", () => {
    const repo = path.join(tmpBase, "my-repo-invalid");
    const al = path.join(repo, ".agent-layer");
    fs.mkdirSync(al, { recursive: true });
    fs.writeFileSync(path.join(al, ".env"), "PARENT_ROOT=/nope\n");

    assert.throws(() => {
      resolveParentRoot({
        agentLayerRoot: al,
      });
    }, /Parent root path does not exist/);
  });

  test("resolveParentRoot throws if dev repo used without flags", () => {
    // Setup: /tmp/agent-layer (not .agent-layer)
    const al = path.join(tmpBase, "agent-layer");
    fs.mkdirSync(al);

    assert.throws(() => {
      resolveParentRoot({
        agentLayerRoot: al,
      });
    }, /Running from agent-layer repo requires explicit parent root/);
  });

  test("resolveParentRoot throws if agent layer root is filesystem root", () => {
    assert.throws(() => {
      resolveParentRoot({
        agentLayerRoot: "/",
      });
    }, /Agent layer root is the filesystem root/);
  });
});
