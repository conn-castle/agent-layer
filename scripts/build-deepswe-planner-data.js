#!/usr/bin/env node
"use strict";

const crypto = require("crypto");
const fs = require("fs");

const SOURCE_URL = "https://deepswe.datacurve.ai/artifacts/v1.1/trials.json";
const REASONING_ORDER = new Map(
  ["low", "medium", "high", "xhigh", "max", "n/a"].map((value, index) => [
    value,
    index,
  ]),
);

/**
 * Parse named command-line arguments and reject unknown or missing inputs.
 * @param {string[]} argv command-line arguments after the Node executable and script
 * @returns {{trialsPath:string,tasksPath:string,outputPath:string,retrievedAt:string}}
 */
function parseArguments(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index];
    const value = argv[index + 1];
    if (!name?.startsWith("--") || value === undefined) {
      throw new Error(
        "Usage: build-deepswe-planner-data.js --trials PATH --tasks PATH --output PATH --retrieved-at ISO_UTC",
      );
    }
    values.set(name, value);
  }
  const allowed = new Set([
    "--trials",
    "--tasks",
    "--output",
    "--retrieved-at",
  ]);
  for (const name of values.keys()) {
    if (!allowed.has(name)) throw new Error(`Unknown argument: ${name}`);
  }
  for (const name of allowed) {
    if (!values.has(name)) throw new Error(`Missing required argument: ${name}`);
  }
  const retrievedAt = values.get("--retrieved-at");
  const parsedDate = new Date(retrievedAt);
  if (
    !Number.isFinite(parsedDate.getTime()) ||
    parsedDate.toISOString() !== retrievedAt
  ) {
    throw new Error("--retrieved-at must be an exact ISO-8601 UTC timestamp.");
  }
  return {
    trialsPath: values.get("--trials"),
    tasksPath: values.get("--tasks"),
    outputPath: values.get("--output"),
    retrievedAt,
  };
}

/**
 * Read and parse a JSON file with an actionable failure.
 * @param {string} path filesystem path
 * @returns {unknown} parsed JSON value
 */
function readJson(path) {
  try {
    return JSON.parse(fs.readFileSync(path, "utf8"));
  } catch (error) {
    throw new Error(`Cannot read valid JSON from ${path}: ${error.message}`);
  }
}

/**
 * Require a non-empty string field.
 * @param {unknown} value candidate value
 * @param {string} context field context for an error
 * @returns {string} validated string
 */
function requireString(value, context) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${context} must be a non-empty string.`);
  }
  return value;
}

/**
 * Return the explicit reason a source trial is unusable, or null when usable.
 * @param {Record<string, unknown>} row source trial
 * @returns {string|null} exclusion reason
 */
function trialExclusionReason(row) {
  if (row.included_in_score !== true) return "not_included_in_score";
  if (row.errored === true) return "errored";
  if (
    !Number.isFinite(row.f2p_passed) ||
    !Number.isFinite(row.f2p_total) ||
    row.f2p_total <= 0
  ) {
    return "invalid_f2p_counts";
  }
  if (!Number.isFinite(row.cost_usd) || row.cost_usd < 0) {
    return "invalid_cost";
  }
  return null;
}

/**
 * Calculate arithmetic mean.
 * @param {number[]} values finite values
 * @returns {number} arithmetic mean
 */
function mean(values) {
  return values.reduce((sum, value) => sum + value, 0) / values.length;
}

/**
 * Calculate sample variance with an n-1 denominator.
 * @param {number[]} values at least two finite values
 * @returns {number} sample variance
 */
function sampleVariance(values) {
  const average = mean(values);
  return (
    values.reduce((sum, value) => sum + (value - average) ** 2, 0) /
    (values.length - 1)
  );
}

/**
 * Sort configuration records deterministically.
 * @param {{model:string,reasoning:string}[]} configs configurations
 * @returns {{model:string,reasoning:string}[]} sorted copy
 */
function sortConfigurations(configs) {
  return [...configs].sort(
    (left, right) =>
      left.model.localeCompare(right.model) ||
      (REASONING_ORDER.get(left.reasoning) ?? 999) -
        (REASONING_ORDER.get(right.reasoning) ?? 999) ||
      left.reasoning.localeCompare(right.reasoning),
  );
}

/**
 * Build the compact, self-describing planner snapshot.
 * @param {Record<string, unknown>} trialsRoot source trials document
 * @param {Record<string, unknown>} tasksRoot task metadata document
 * @param {{sourceSha256:string,retrievedAt:string}} provenance source metadata
 * @returns {Record<string, unknown>} generated planner snapshot
 */
function buildSnapshot(trialsRoot, tasksRoot, provenance) {
  if (!Array.isArray(trialsRoot.rows)) {
    throw new Error("Trials JSON must contain a rows array.");
  }
  if (!Array.isArray(tasksRoot.rows)) {
    throw new Error("Tasks JSON must contain a rows array.");
  }

  const taskMetadata = new Map();
  for (const task of tasksRoot.rows) {
    const id = requireString(task.id, "task.id");
    if (taskMetadata.has(id)) throw new Error(`Duplicate task metadata: ${id}`);
    taskMetadata.set(id, {
      id,
      language: requireString(task.language, `${id}.language`),
      title: requireString(task.problem_title, `${id}.problem_title`),
      description: requireString(
        task.display_description,
        `${id}.display_description`,
      ),
      repository: requireString(task.repository, `${id}.repository`),
      repositoryUrl: requireString(
        task.repository_url,
        `${id}.repository_url`,
      ),
    });
  }

  const configurations = new Map();
  const cells = new Map();
  const excludedTrials = [];

  for (const row of trialsRoot.rows) {
    if (!row || typeof row !== "object" || Array.isArray(row)) {
      throw new Error("Every trial row must be an object.");
    }
    const trialId = requireString(row.trial_name, "trial.trial_name");
    const taskId = requireString(row.task_name, `${trialId}.task_name`);
    const model = requireString(row.model, `${trialId}.model`);
    const reasoning =
      typeof row.reasoning_effort === "string" &&
      row.reasoning_effort.trim() !== ""
        ? row.reasoning_effort
        : "n/a";
    const configId = `${model}::${reasoning}`;
    const exclusionReason = trialExclusionReason(row);
    if (exclusionReason) {
      excludedTrials.push({
        id: trialId,
        task: taskId,
        configuration: configId,
        reason: exclusionReason,
      });
      continue;
    }
    if (!taskMetadata.has(taskId)) {
      throw new Error(`${trialId} references unknown task metadata: ${taskId}`);
    }
    if (!configurations.has(configId)) {
      configurations.set(configId, {
        id: configId,
        model,
        reasoning,
        providers: new Set(),
        harnesses: new Set(),
        trialCount: 0,
        taskCount: 0,
      });
    }
    const configuration = configurations.get(configId);
    configuration.providers.add(
      requireString(row.provider, `${trialId}.provider`),
    );
    configuration.harnesses.add(
      requireString(row.harness, `${trialId}.harness`),
    );
    configuration.trialCount += 1;

    const cellId = `${taskId}\u0000${configId}`;
    if (!cells.has(cellId)) cells.set(cellId, []);
    const passed = Number(row.f2p_passed);
    const total = Number(row.f2p_total);
    cells.get(cellId).push({
      id: trialId,
      passed,
      total,
      score: passed / total,
      cost: Number(row.cost_usd),
    });
  }

  const taskCells = new Map();
  for (const [cellId, trials] of cells.entries()) {
    const separator = cellId.indexOf("\u0000");
    const taskId = cellId.slice(0, separator);
    const configId = cellId.slice(separator + 1);
    const scores = trials.map((trial) => trial.score);
    const costs = trials.map((trial) => trial.cost);
    const variance = trials.length >= 2 ? sampleVariance(scores) : null;
    if (!taskCells.has(taskId)) taskCells.set(taskId, {});
    taskCells.get(taskId)[configId] = {
      n: trials.length,
      mean: mean(scores),
      variance,
      sd: variance === null ? null : Math.sqrt(variance),
      meanCost: mean(costs),
      minCost: Math.min(...costs),
      maxCost: Math.max(...costs),
      trials: [...trials].sort((left, right) => left.id.localeCompare(right.id)),
    };
    configurations.get(configId).taskCount += 1;
  }

  const configList = sortConfigurations([...configurations.values()]).map(
    (configuration) => ({
      ...configuration,
      providers: [...configuration.providers].sort(),
      harnesses: [...configuration.harnesses].sort(),
    }),
  );
  const tasks = [...taskMetadata.values()]
    .sort((left, right) => left.id.localeCompare(right.id))
    .map((task) => ({ ...task, cells: taskCells.get(task.id) ?? {} }));

  const exclusionCounts = {};
  for (const trial of excludedTrials) {
    exclusionCounts[trial.reason] = (exclusionCounts[trial.reason] ?? 0) + 1;
  }
  const usableTrialCount = [...cells.values()].reduce(
    (sum, trials) => sum + trials.length,
    0,
  );
  const modelCount = new Set(configList.map((configuration) => configuration.model)).size;

  return {
    schemaVersion: 1,
    source: {
      url: SOURCE_URL,
      inputPath: SOURCE_URL,
      retrievedAt: provenance.retrievedAt,
      sha256: provenance.sourceSha256,
      sourceTrialCount: trialsRoot.rows.length,
      usableTrialCount,
      excludedTrialCount: excludedTrials.length,
      modelCount,
      configurationCount: configList.length,
      taskCount: tasks.length,
    },
    configurations: configList,
    tasks,
    exclusions: {
      trialCounts: exclusionCounts,
    },
  };
}

const args = parseArguments(process.argv.slice(2));
const sourceBytes = fs.readFileSync(args.trialsPath);
const snapshot = buildSnapshot(
  JSON.parse(sourceBytes.toString("utf8")),
  readJson(args.tasksPath),
  {
    sourceSha256: crypto.createHash("sha256").update(sourceBytes).digest("hex"),
    retrievedAt: args.retrievedAt,
  },
);
const serialized = JSON.stringify(snapshot).replaceAll("<", "\\u003c");
fs.writeFileSync(
  args.outputPath,
  `"use strict";\nwindow.__DEEPSWE_PLANNER_DATA__=${serialized};\n`,
);
console.log(
  JSON.stringify({
    output: args.outputPath,
    source: snapshot.source,
    bytes: fs.statSync(args.outputPath).size,
  }),
);
