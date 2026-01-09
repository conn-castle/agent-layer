import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  ListPromptsRequestSchema,
  GetPromptRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

function parseFrontMatter(markdown) {
  const lines = markdown.split(/\r?\n/);
  if (lines[0] !== "---") return { meta: {}, body: markdown };
  const meta = {};
  let i = 1;
  for (; i < lines.length; i++) {
    const line = lines[i];
    if (line === "---") break;
    const idx = line.indexOf(":");
    if (idx === -1) continue;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    meta[k] = v.replace(/^["']|["']$/g, "");
  }
  const body = lines.slice(i + 1).join("\n").replace(/^\n+/, "");
  return { meta, body };
}

function findWorkflowsDir(startDir) {
  let dir = path.resolve(startDir);
  for (let i = 0; i < 50; i++) {
    const wf = path.join(dir, ".agent-layer", "workflows");
    if (fs.existsSync(wf)) return wf;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WORKFLOWS_DIR = findWorkflowsDir(process.cwd()) ?? findWorkflowsDir(HERE);

function loadWorkflows() {
  if (!WORKFLOWS_DIR) return [];
  let files = [];
  try {
    files = fs.readdirSync(WORKFLOWS_DIR);
  } catch {
    return [];
  }
  return files
    .filter((f) => f.endsWith(".md"))
    .sort()
    .map((f) => {
      const full = path.join(WORKFLOWS_DIR, f);
      const md = fs.readFileSync(full, "utf8");
      const { meta, body } = parseFrontMatter(md);
      const name = meta.name || path.basename(f, ".md");
      const description = meta.description || "";
      return { name, description, body };
    });
}

const server = new Server(
  { name: "agent-layer-prompts", version: "0.1.0" },
  { capabilities: { prompts: {} } }
);

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
            `- If you modify .agent-layer/**, run: node .agent-layer/sync/sync.mjs\n`,
        },
      },
    ],
  };
});

async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error(err);
  process.exit(1);
});
