import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  ListPromptsRequestSchema,
  GetPromptRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { parseFrontMatter } from "../../sync/instructions.mjs";
import { resolveRootsFromEnvOrScript } from "../../sync/paths.mjs";

/**
 * MCP prompt server that exposes workflow markdown as prompt definitions.
 */

// Resolve the workflow directory relative to the repo root.
const ENTRY_PATH = process.argv[1] ?? fileURLToPath(import.meta.url);
const ROOTS = resolveRootsFromEnvOrScript(ENTRY_PATH);
const AGENT_LAYER_ROOT = ROOTS ? ROOTS.agentLayerRoot : null;
const WORKFLOWS_DIR = AGENT_LAYER_ROOT
  ? path.join(AGENT_LAYER_ROOT, "config", "workflows")
  : null;

/**
 * List workflow markdown files from the workflows directory.
 * @param {string | null} workflowsDir
 * @returns {string[]}
 */
function listWorkflowFiles(workflowsDir) {
  if (!workflowsDir) {
    throw new Error(
      "agent-layer prompts: could not find .agent-layer/config/workflows. " +
        "Run from a repo that contains .agent-layer or fix the MCP server path.",
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
 * @returns {{ name: string, description: string, body: string }[]}
 */
function loadWorkflows() {
  const files = listWorkflowFiles(WORKFLOWS_DIR);
  return files.map((f) => {
    const full = path.join(WORKFLOWS_DIR, f);
    const md = fs.readFileSync(full, "utf8");
    const { meta, body } = parseFrontMatter(md, full);
    const name = meta.name || path.basename(f, ".md");
    const description = meta.description || "";
    return { name, description, body };
  });
}

// Instantiate the MCP server with prompt/tool capabilities enabled.
const server = new Server(
  { name: "agent-layer-prompts", version: "0.1.0" },
  { capabilities: { prompts: {}, tools: {} } },
);

/**
 * Return an empty tool list to satisfy MCP clients that probe for tools.
 * @returns {{ tools: [] }}
 */
function listTools() {
  return { tools: [] };
}

// Register handlers for the MCP prompt/tool request types.
server.setRequestHandler(ListToolsRequestSchema, async () => listTools());

server.setRequestHandler(ListPromptsRequestSchema, async () => {
  const workflows = loadWorkflows();
  return {
    prompts: workflows.map((w) => ({
      name: w.name,
      description: w.description,
    })),
  };
});

server.setRequestHandler(GetPromptRequestSchema, async (request) => {
  const name = request.params?.name;
  const workflows = loadWorkflows();
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
            `- If you modify .agent-layer/**, run: node .agent-layer/src/sync/sync.mjs\n`,
        },
      },
    ],
  };
});

// Validate inputs and start the stdio server.
async function main() {
  listWorkflowFiles(WORKFLOWS_DIR);
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error(err);
  process.exit(1);
});
