import path from "node:path";
import process from "node:process";
import readline from "node:readline/promises";

/**
 * Format a file path relative to the parent root, preserving line suffixes.
 * @param {string} filePath
 * @param {string} parentRoot
 * @returns {string}
 */
function formatRelativePath(filePath, parentRoot) {
  if (!filePath) return filePath;
  const match = filePath.match(/^(.*):(\d+)$/);
  const rawPath = match ? match[1] : filePath;
  const suffix = match ? `:${match[2]}` : "";
  const relative =
    parentRoot && rawPath.startsWith(parentRoot)
      ? path.relative(parentRoot, rawPath)
      : rawPath;
  return `${relative || rawPath}${suffix}`;
}

/**
 * Format divergence details for interactive prompts.
 * @param {import("./divergence.mjs").DivergenceResult} divergence
 * @param {string} parentRoot
 * @returns {string}
 */
function formatDivergenceDetails(divergence, parentRoot) {
  const lines = [];
  if (divergence.approvals.length) {
    lines.push("Divergent approvals:");
    for (const item of divergence.approvals) {
      const label = item.prefix ?? item.raw ?? "<unparseable entry>";
      const reason = item.parseable
        ? ""
        : ` (unparseable: ${item.reason ?? "unknown"})`;
      const filePath = formatRelativePath(item.filePath, parentRoot);
      const fileNote = filePath ? ` (file: ${filePath})` : "";
      lines.push(`- ${item.source}: ${label}${fileNote}${reason}`);
    }
  }

  if (divergence.mcp.length) {
    if (lines.length) lines.push("");
    lines.push("Divergent MCP servers:");
    for (const item of divergence.mcp) {
      const filePath = formatRelativePath(item.filePath, parentRoot);
      const detailParts = [];
      if (item.parseable && item.server) {
        const args = item.server.args?.length
          ? ` ${item.server.args.join(" ")}`
          : "";
        detailParts.push(`${item.server.command}${args}`.trim());
        if (item.server.envVarsKnown && item.server.envVars.length) {
          detailParts.push(`env=${item.server.envVars.join(",")}`);
        }
        if (item.server.trust !== undefined) {
          detailParts.push(`trust=${item.server.trust}`);
        }
      } else if (item.reason) {
        detailParts.push(`unparseable: ${item.reason}`);
      } else {
        detailParts.push("unparseable entry");
      }
      if (filePath) detailParts.push(`file: ${filePath}`);
      const detail =
        detailParts.length > 0 ? ` (${detailParts.join(", ")})` : "";
      lines.push(`- ${item.source}: ${item.name}${detail}`);
    }
  }

  return lines.join("\n");
}

/**
 * Prompt for how to handle divergence when running interactively.
 * @param {import("./divergence.mjs").DivergenceResult} divergence
 * @param {string} parentRoot
 * @returns {Promise<"overwrite" | "abort">}
 */
export async function promptDivergenceAction(divergence, parentRoot) {
  if (!process.stdin.isTTY || !process.stdout.isTTY) {
    console.error("agent-layer sync: --interactive requires a TTY.");
    process.exit(2);
  }

  const parts = [];
  if (divergence.approvals.length) {
    parts.push(`approvals: ${divergence.approvals.length}`);
  }
  if (divergence.mcp.length) parts.push(`mcp: ${divergence.mcp.length}`);
  const detail = parts.length ? ` (${parts.join(", ")})` : "";

  console.warn(
    `agent-layer sync: WARNING: client configs diverge from .agent-layer sources${detail}.`,
  );
  console.warn("");
  console.warn("Details:");
  console.warn(formatDivergenceDetails(divergence, parentRoot));
  console.warn("");
  console.warn("Notes:");
  console.warn(
    "- Sync preserves existing client entries by default; it will not overwrite them unless you pass --overwrite or choose overwrite in --interactive.",
  );
  console.warn("");
  console.warn("Next steps:");
  console.warn(
    "- Run: node .agent-layer/src/sync/inspect.mjs (JSON report) to see what differs, then update .agent-layer sources.",
  );
  console.warn("- Or choose overwrite to discard client-only entries now.");
  console.warn("");

  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });
  const answer = await rl.question(
    "Choose: [1] stop and update Agent Layer, [2] overwrite client configs now: ",
  );
  rl.close();

  const choice = answer.trim();
  if (choice === "2") return "overwrite";
  return "abort";
}
