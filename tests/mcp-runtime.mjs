import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  InitializeRequestSchema,
  InitializedNotificationSchema,
  ListPromptsRequestSchema,
  ListToolsRequestSchema,
  LATEST_PROTOCOL_VERSION,
} from "../mcp/agent-layer-prompts/node_modules/@modelcontextprotocol/sdk/dist/esm/types.js";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const AGENTLAYER_ROOT = path.resolve(HERE, "..");
const REPO_ROOT = path.resolve(AGENTLAYER_ROOT, "..");
const SERVER_PATH = path.join(AGENTLAYER_ROOT, "mcp", "agent-layer-prompts", "server.mjs");

/**
 * Extract a Zod schema shape, if available.
 * @param {unknown} schema
 * @returns {Record<string, unknown> | null}
 */
function getShape(schema) {
  if (schema?.shape) return schema.shape;
  const def = schema?._def;
  if (!def) return null;
  if (typeof def.shape === "function") return def.shape();
  return def.shape ?? null;
}

/**
 * Extract a literal value from a Zod schema, if available.
 * @param {unknown} schema
 * @returns {unknown}
 */
function getLiteralValue(schema) {
  if (schema?.value !== undefined) return schema.value;
  if (schema?._def?.value !== undefined) return schema._def.value;
  return null;
}

/**
 * Resolve the JSON-RPC method name from a schema.
 * @param {unknown} schema
 * @returns {string}
 */
function getMethod(schema) {
  const shape = getShape(schema);
  const methodSchema = shape?.method;
  const value = getLiteralValue(methodSchema);
  if (!value) {
    throw new Error("Unable to determine MCP method name from schema.");
  }
  return value;
}

/**
 * Spawn the MCP prompt server process.
 * @returns {import("node:child_process").ChildProcess}
 */
function createTransport() {
  const child = spawn(process.execPath, [SERVER_PATH], {
    cwd: REPO_ROOT,
    stdio: ["pipe", "pipe", "pipe"],
  });
  return child;
}

/**
 * Exercise initialize, list prompts, and list tools.
 * @returns {Promise<void>}
 */
async function run() {
  const child = createTransport();
  const pending = new Map();
  let buffer = "";
  let stderr = "";

  const cleanup = () => {
    child.stdin.end();
    child.kill("SIGTERM");
  };

  const fail = (err) => {
    cleanup();
    if (stderr) err.message += `\nServer stderr:\n${stderr}`;
    throw err;
  };

  /**
   * Wait for a response with a matching id.
   * @param {number} id
   * @param {number} timeoutMs
   * @returns {Promise<{id: number, result?: unknown, error?: unknown}>}
   */
  const waitForResponse = (id, timeoutMs = 2000) =>
    new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        pending.delete(id);
        reject(new Error(`Timed out waiting for response to id ${id}.`));
      }, timeoutMs);
      pending.set(id, {
        resolve: (msg) => {
          clearTimeout(timeout);
          resolve(msg);
        },
        reject: (err) => {
          clearTimeout(timeout);
          reject(err);
        },
      });
    });

  child.stderr.on("data", (chunk) => {
    stderr += chunk.toString("utf8");
  });

  child.stdout.on("data", (chunk) => {
    buffer += chunk.toString("utf8");
    let idx;
    while ((idx = buffer.indexOf("\n")) !== -1) {
      const line = buffer.slice(0, idx).trim();
      buffer = buffer.slice(idx + 1);
      if (!line) continue;
      let msg;
      try {
        msg = JSON.parse(line);
      } catch {
        continue;
      }
      if (msg?.id !== undefined && pending.has(msg.id)) {
        pending.get(msg.id).resolve(msg);
      }
    }
  });

  child.on("exit", (code, signal) => {
    if (pending.size > 0) {
      const err = new Error(
        `Server exited before completing requests (code=${code}, signal=${signal}).`
      );
      for (const { reject } of pending.values()) {
        reject(err);
      }
      pending.clear();
    }
  });

  /**
   * Send a JSON-RPC message to the child process.
   * @param {Record<string, unknown>} msg
   * @returns {void}
   */
  const sendMessage = (msg) => {
    child.stdin.write(`${JSON.stringify(msg)}\n`);
  };

  const initializeId = 1;
  sendMessage({
    jsonrpc: "2.0",
    id: initializeId,
    method: getMethod(InitializeRequestSchema),
    params: {
      protocolVersion: LATEST_PROTOCOL_VERSION,
      capabilities: {},
      clientInfo: { name: "agent-layer-tests", version: "0.0.0" },
    },
  });
  const initResponse = await waitForResponse(initializeId).catch(fail);
  if (!initResponse?.result?.capabilities) {
    fail(new Error("Initialize response missing capabilities."));
  }

  sendMessage({
    jsonrpc: "2.0",
    method: getMethod(InitializedNotificationSchema),
  });

  const promptsId = 2;
  sendMessage({
    jsonrpc: "2.0",
    id: promptsId,
    method: getMethod(ListPromptsRequestSchema),
    params: {},
  });
  const promptsResponse = await waitForResponse(promptsId).catch(fail);
  if (!Array.isArray(promptsResponse?.result?.prompts)) {
    fail(new Error("Prompts list response missing prompts array."));
  }

  const toolsId = 3;
  sendMessage({
    jsonrpc: "2.0",
    id: toolsId,
    method: getMethod(ListToolsRequestSchema),
    params: {},
  });
  const toolsResponse = await waitForResponse(toolsId).catch(fail);
  if (!Array.isArray(toolsResponse?.result?.tools)) {
    fail(new Error("Tools list response missing tools array."));
  }

  cleanup();
}

run().catch((err) => {
  // eslint-disable-next-line no-console
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
