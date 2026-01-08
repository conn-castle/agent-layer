/**
 * Command used to regenerate generated files.
 * @type {string}
 */
export const REGEN_COMMAND = "node .agentlayer/sync/sync.mjs";

/**
 * Legacy regenerate command retained for recognizing older generated files.
 * @type {string}
 */
export const LEGACY_REGEN_COMMANDS = [
  "node .agentlayer/sync.mjs",
  "node sync/sync.mjs",
];
