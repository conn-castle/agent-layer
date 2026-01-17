import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  GetPromptRequestSchema,
  ListPromptsRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { parseFrontMatter } from "../../sync/instructions.mjs";

/**
 * MCP prompt server that exposes workflow markdown as prompt definitions.
 */

/**
 * Resolve the server version by matching the launcher git describe behavior.
 * @param {string} agentLayerRoot
 * @returns {string}
 */
function resolveServerVersion(agentLayerRoot) {
  if (!agentLayerRoot) {
    throw new Error("agent-layer prompts: agentLayerRoot is required.");
  }
  const result = spawnSync(
    "git",
    ["-C", agentLayerRoot, "describe", "--tags", "--always", "--dirty"],
    { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
  );
  const version = result.status === 0 ? result.stdout.trim() : "";
  return version || "unknown";
}

/**
 * List workflow markdown files from the workflows directory.
 * @param {string} workflowsDir
 * @returns {string[]}
 */
function listWorkflowFiles(workflowsDir) {
  if (!workflowsDir) {
    throw new Error(
      "agent-layer prompts: missing workflows directory. " +
        "Set --agent-layer-root to a repo that contains .agent-layer.",
    );
  }
  let files;
  try {
    files = fs.readdirSync(workflowsDir);
  } catch {
    throw new Error(
      `agent-layer prompts: unable to read workflows directory at ${workflowsDir}.`,
    );
  }
  const markdown = files.filter((f) => f.endsWith(".md")).sort();
  if (markdown.length === 0) {
    throw new Error(
      `agent-layer prompts: no workflow files found in ${workflowsDir}. ` +
        "Add at least one workflow markdown file.",
    );
  }
  return markdown;
}

/**
 * Load workflow prompt definitions from disk.
 * @param {string} workflowsDir
 * @returns {{ name: string, description: string, body: string }[]}
 */
function loadWorkflows(workflowsDir) {
  const files = listWorkflowFiles(workflowsDir);
  return files.map((f) => {
    const full = path.join(workflowsDir, f);
    const md = fs.readFileSync(full, "utf8");
    const { meta, body } = parseFrontMatter(md, full);
    const name = meta.name || path.basename(f, ".md");
    const description = meta.description || "";
    return { name, description, body };
  });
}

/**
 * Start the MCP prompt server.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @returns {Promise<void>}
 */
export async function runPromptServer(parentRoot, agentLayerRoot) {
  if (!agentLayerRoot || typeof agentLayerRoot !== "string") {
    throw new Error("agent-layer prompts: agentLayerRoot is required.");
  }

  const workflowsDir = path.join(agentLayerRoot, "config", "workflows");
  listWorkflowFiles(workflowsDir);
  const serverVersion = resolveServerVersion(agentLayerRoot);

  const server = new Server(
    { name: "agent-layer-prompts", version: serverVersion },
    { capabilities: { prompts: {}, tools: {} } },
  );

  /**
   * Return an empty tool list to satisfy MCP clients that probe for tools.
   * @returns {{ tools: [] }}
   */
  function listTools() {
    return { tools: [] };
  }

  server.setRequestHandler(ListToolsRequestSchema, async () => listTools());

  server.setRequestHandler(ListPromptsRequestSchema, async () => {
    const workflows = loadWorkflows(workflowsDir);
    return {
      prompts: workflows.map((w) => ({
        name: w.name,
        description: w.description,
      })),
    };
  });

  server.setRequestHandler(GetPromptRequestSchema, async (request) => {
    const name = request.params?.name;
    const workflows = loadWorkflows(workflowsDir);
    const w = workflows.find((x) => x.name === name);
    if (!w) {
      return {
        description: "Unknown workflow",
        messages: [
          {
            role: "user",
            content: { type: "text", text: `Unknown workflow: ${name}` },
          },
        ],
      };
    }

    return {
      description: w.description,
      messages: [
        {
          role: "user",
          content: {
            type: "text",
            text:
              `${w.body.trim()}\n\n` +
              `---\n` +
              `Notes:\n` +
              `- Follow the workflow exactly.\n` +
              `- If you modify .agent-layer/**, run: ./al --sync\n`,
          },
        },
      ],
    };
  });

  const transport = new StdioServerTransport();
  await server.connect(transport);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  console.error("agent-layer prompts: use ./al --mcp-prompts");
  process.exit(2);
}
