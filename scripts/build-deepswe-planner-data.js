#!/usr/bin/env node
"use strict";

const crypto = require("crypto");
const fs = require("fs");

const SOURCE_URL = "https://deepswe.datacurve.ai/artifacts/v1.1/trials.json";
const CORRELATION_SAMPLES = 10_000;
const COMPLETE_CELL_RUNS = 4;
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
 * Calculate a Pearson correlation, returning zero for a constant vector.
 * @param {number[]} left first finite vector
 * @param {number[]} right second finite vector of equal length
 * @returns {number} Pearson correlation
 */
function pearsonCorrelation(left, right) {
  if (left.length !== right.length || left.length < 2) {
    throw new Error("Pearson correlation requires equal vectors of length two or greater.");
  }
  const leftMean = mean(left);
  const rightMean = mean(right);
  let covariance = 0;
  let leftSquares = 0;
  let rightSquares = 0;
  for (let index = 0; index < left.length; index += 1) {
    const leftDelta = left[index] - leftMean;
    const rightDelta = right[index] - rightMean;
    covariance += leftDelta * rightDelta;
    leftSquares += leftDelta * leftDelta;
    rightSquares += rightDelta * rightDelta;
  }
  const denominator = Math.sqrt(leftSquares * rightSquares);
  return denominator > 0 ? covariance / denominator : 0;
}

/**
 * Calculate a type-7 empirical quantile from sorted finite values.
 * @param {number[]} sortedValues ascending finite values
 * @param {number} probability probability from zero through one
 * @returns {number} interpolated quantile
 */
function quantile(sortedValues, probability) {
  if (sortedValues.length === 0) throw new Error("Quantile requires values.");
  const position = (sortedValues.length - 1) * probability;
  const lower = Math.floor(position);
  const fraction = position - lower;
  const upper = sortedValues[Math.min(lower + 1, sortedValues.length - 1)];
  return sortedValues[lower] + fraction * (upper - sortedValues[lower]);
}

/**
 * Create a deterministic unsigned 32-bit generator.
 * @param {number} seed unsigned 32-bit seed
 * @returns {() => number} next unsigned 32-bit value
 */
function createGenerator(seed) {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return (value ^ (value >>> 14)) >>> 0;
  };
}

/**
 * Calculate one task's deterministic one-run correlation distribution.
 * @param {object} task generated task with cells
 * @param {Map<string, number>} fullScores fixed full DeepSWE score by configuration
 * @param {string} sourceSha256 pinned source digest
 * @returns {object} compact correlation evidence
 */
function calculateTaskCorrelation(task, fullScores, sourceSha256) {
  const completeCells = Object.entries(task.cells)
    .filter(
      ([configuration, cell]) =>
        cell.n === COMPLETE_CELL_RUNS && fullScores.has(configuration),
    )
    .sort(([left], [right]) => left.localeCompare(right));
  if (completeCells.length < 2) {
    return {
      completeConfigurationCount: completeCells.length,
      median: null,
      p05: null,
      p95: null,
    };
  }
  const fixedScores = completeCells.map(([configuration]) =>
    fullScores.get(configuration),
  );
  const seed = crypto
    .createHash("sha256")
    .update(`${sourceSha256}\u0000${task.id}`)
    .digest()
    .readUInt32LE(0);
  const nextValue = createGenerator(seed);
  const sampledScores = Array(completeCells.length);
  const correlations = Array(CORRELATION_SAMPLES);
  for (let sample = 0; sample < CORRELATION_SAMPLES; sample += 1) {
    for (let index = 0; index < completeCells.length; index += 1) {
      const trials = completeCells[index][1].trials;
      sampledScores[index] =
        trials[nextValue() % COMPLETE_CELL_RUNS].score;
    }
    correlations[sample] = pearsonCorrelation(sampledScores, fixedScores);
  }
  correlations.sort((left, right) => left - right);
  return {
    completeConfigurationCount: completeCells.length,
    median: quantile(correlations, 0.5),
    p05: quantile(correlations, 0.05),
    p95: quantile(correlations, 0.95),
  };
}

/**
 * Fit one task's F2P mean to the fixed full DeepSWE configuration score.
 * @param {object} task generated task with cells
 * @param {Map<string, number>} fullScores fixed full DeepSWE score by configuration
 * @returns {object|null} ordinary least-squares calibration and precision
 */
function calculateTaskCalibration(task, fullScores) {
  const observations = Object.entries(task.cells)
    .filter(
      ([configuration, cell]) =>
        cell.n === COMPLETE_CELL_RUNS && fullScores.has(configuration),
    )
    .map(([configuration, cell]) => ({
      predictor: cell.mean,
      target: fullScores.get(configuration),
    }));
  if (observations.length < 3) return null;

  const predictorMean = mean(
    observations.map((observation) => observation.predictor),
  );
  const targetMean = mean(
    observations.map((observation) => observation.target),
  );
  let crossProduct = 0;
  let predictorSquares = 0;
  for (const observation of observations) {
    const predictorDelta = observation.predictor - predictorMean;
    crossProduct += predictorDelta * (observation.target - targetMean);
    predictorSquares += predictorDelta * predictorDelta;
  }
  if (predictorSquares <= 0) return null;

  const slope = crossProduct / predictorSquares;
  const intercept = targetMean - slope * predictorMean;
  const residualSquares = observations.reduce((sum, observation) => {
    const residual =
      observation.target -
      (intercept + slope * observation.predictor);
    return sum + residual * residual;
  }, 0);
  const residualVariance =
    residualSquares / (observations.length - 2);
  if (!Number.isFinite(residualVariance) || residualVariance <= 0) return null;

  return {
    configurationCount: observations.length,
    intercept,
    slope,
    residualVariance,
    precisionWeight: 1 / residualVariance,
  };
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
  const fullScoreTrials = new Map();
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
    if (row.included_in_score === true) {
      if (!Number.isFinite(row.score_value)) {
        throw new Error(`${trialId}.score_value must be finite when included in score.`);
      }
      if (!fullScoreTrials.has(configId)) fullScoreTrials.set(configId, []);
      fullScoreTrials.get(configId).push(Number(row.score_value));
    }
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
      deepSWEScore: mean(fullScoreTrials.get(configuration.id)),
      providers: [...configuration.providers].sort(),
      harnesses: [...configuration.harnesses].sort(),
    }),
  );
  const fullScores = new Map(
    configList.map((configuration) => [
      configuration.id,
      configuration.deepSWEScore,
    ]),
  );
  const tasks = [...taskMetadata.values()]
    .sort((left, right) => left.id.localeCompare(right.id))
    .map((task) => ({ ...task, cells: taskCells.get(task.id) ?? {} }))
    .map((task) => ({
      ...task,
      correlation: calculateTaskCorrelation(
        task,
        fullScores,
        provenance.sourceSha256,
      ),
      calibration: calculateTaskCalibration(task, fullScores),
    }));

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
    schemaVersion: 3,
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
    correlationMethod: {
      id: "one-run-pearson-monte-carlo-v1",
      samples: CORRELATION_SAMPLES,
      completeCellRuns: COMPLETE_CELL_RUNS,
      seed: "sha256(snapshot-sha256 + NUL + task-id), first uint32 little-endian",
      rankingStatistic: "p05",
      interval: ["p05", "p95"],
      fixedScore: "mean score_value across all included trials per configuration",
    },
    calibrationMethod: {
      id: "task-ols-inverse-residual-variance-v1",
      predictor: "mean F2P score for each complete four-run configuration cell",
      target: "fixed full DeepSWE configuration score",
      fit: "ordinary least squares with intercept",
      residualVariance: "residual sum of squares divided by configuration count minus two",
      combination: "normalized inverse residual-variance weighted mean",
    },
    exclusions: {
      trialCounts: exclusionCounts,
    },
  };
}

if (require.main === module) {
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
}

module.exports = {
  buildSnapshot,
  calculateTaskCalibration,
  calculateTaskCorrelation,
  pearsonCorrelation,
  quantile,
};
