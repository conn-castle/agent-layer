#!/usr/bin/env bats

# Tests for JSON parsing helpers and policy validation.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: utils helpers strip JSONC comments and trailing commas
@test "utils helpers strip JSONC comments and trailing commas" {
  local tmp file
  tmp="$(make_tmp_dir)"
  file="$tmp/sample.jsonc"

  cat >"$file" <<'EOF'
{
  // line comment
  "a": 1, /* block comment */
  "b": [1, 2,],
  "c": { "d": "ok", },
}
EOF

  UTILS_PATH="$AGENT_LAYER_ROOT/src/sync/utils.mjs" run node --input-type=module -e '
import fs from "node:fs";
import { pathToFileURL } from "node:url";

const { stripJsoncComments, removeTrailingCommas } = await import(
  pathToFileURL(process.env.UTILS_PATH).href,
);
const raw = fs.readFileSync(process.argv[1], "utf8");
const stripped = stripJsoncComments(raw);
if (stripped.includes("//") || stripped.includes("/*")) {
  throw new Error("Comments were not stripped.");
}
const cleaned = removeTrailingCommas(stripped);
const parsed = JSON.parse(cleaned);
if (parsed.a !== 1 || parsed.b.length !== 2 || parsed.c.d !== "ok") {
  throw new Error("Parsed JSON does not match expected content.");
}
' "$file"
  [ "$status" -eq 0 ]
}

# Test: readJsonRelaxed accepts JSONC with comments and trailing commas
@test "readJsonRelaxed accepts JSONC with comments and trailing commas" {
  local tmp file
  tmp="$(make_tmp_dir)"
  file="$tmp/relaxed.jsonc"

  cat >"$file" <<'EOF'
{
  "answer": 42,
  // comment
  "values": [1, 2,],
}
EOF

  UTILS_PATH="$AGENT_LAYER_ROOT/src/sync/utils.mjs" run node --input-type=module -e '
import { pathToFileURL } from "node:url";

const { readJsonRelaxed } = await import(
  pathToFileURL(process.env.UTILS_PATH).href,
);
const parsed = readJsonRelaxed(process.argv[1], null);
if (!parsed || parsed.answer !== 42 || parsed.values.length !== 2) {
  throw new Error("readJsonRelaxed did not parse JSONC as expected.");
}
' "$file"
  [ "$status" -eq 0 ]
}

# Test: validateCommandPolicy rejects invalid argv entries
@test "validateCommandPolicy rejects invalid argv entries" {
  POLICY_PATH="$AGENT_LAYER_ROOT/src/sync/policy.mjs" run node --input-type=module -e '
import { pathToFileURL } from "node:url";

const { validateCommandPolicy } = await import(
  pathToFileURL(process.env.POLICY_PATH).href,
);

const badPolicies = [
  { version: 1, allowed: [{ argv: [] }] },
  { version: 1, allowed: [{ argv: ["git", "status|all"] }] },
  { version: 1, allowed: [{ argv: [""] }] },
];

for (const policy of badPolicies) {
  let threw = false;
  try {
    validateCommandPolicy(policy, "policy.json");
  } catch {
    threw = true;
  }
  if (!threw) {
    throw new Error("Expected validation failure for invalid policy.");
  }
}

validateCommandPolicy({ version: 1, allowed: [{ argv: ["git", "status"] }] }, "policy.json");
'
  [ "$status" -eq 0 ]
}
