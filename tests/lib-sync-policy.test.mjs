import { test, describe } from "node:test";
import assert from "node:assert";
import { validateCommandPolicy } from "../src/sync/policy.mjs";

describe("src/sync/policy.mjs", () => {
  test("validateCommandPolicy accepts valid policy", () => {
    const validPolicy = {
      version: 1,
      allowed: [{ argv: ["git", "status"] }],
    };

    // Should not throw
    validateCommandPolicy(validPolicy, "policy.json");
  });

  test("validateCommandPolicy rejects empty argv", () => {
    const policy = {
      version: 1,
      allowed: [{ argv: [] }],
    };

    assert.throws(() => {
      validateCommandPolicy(policy, "policy.json");
    });
  });

  test("validateCommandPolicy rejects argv with shell operators", () => {
    const policy = {
      version: 1,
      allowed: [{ argv: ["git", "status|all"] }],
    };

    assert.throws(() => {
      validateCommandPolicy(policy, "policy.json");
    });
  });

  test("validateCommandPolicy rejects argv with empty strings", () => {
    const policy = {
      version: 1,
      allowed: [{ argv: [""] }],
    };

    assert.throws(() => {
      validateCommandPolicy(policy, "policy.json");
    });
  });

  test("validateCommandPolicy rejects missing version", () => {
    const policy = {
      allowed: [{ argv: ["git"] }],
    };

    assert.throws(() => {
      validateCommandPolicy(policy, "policy.json");
    }, /version/);
  });

  test("validateCommandPolicy rejects non-array allowed", () => {
    const policy = {
      version: 1,
      allowed: "not-an-array",
    };

    assert.throws(() => {
      validateCommandPolicy(policy, "policy.json");
    }, /allowed/);
  });
});
