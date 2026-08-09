#!/usr/bin/env node
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const {
  buildSnapshot,
  pearsonCorrelation,
  quantile,
} = require("./build-deepswe-planner-data.js");
const {
  matrixSelectionID,
} = require("./render-deepswe-matrix-report.js");

const repositoryRoot = path.resolve(__dirname, "..");
const applicationPath = path.join(
  repositoryRoot,
  "site/static/deepswe-planner/app/index.html",
);
const dataPath = path.join(
  repositoryRoot,
  "site/static/deepswe-planner/app/data.js",
);

/**
 * Build one valid published trial fixture.
 * @param {string} task task identifier
 * @param {string} model model identifier
 * @param {number} run zero-based run
 * @param {number} f2p F2P score
 * @param {number} fullScore fixed full DeepSWE score
 * @returns {object} source trial row
 */
function trial(task, model, run, f2p, fullScore) {
  return {
    trial_name: `${task}-${model}-${run}`,
    task_name: task,
    model,
    reasoning_effort: "high",
    provider: "fixture",
    harness: "mini-swe-agent",
    included_in_score: true,
    errored: false,
    score_value: fullScore,
    f2p_passed: Math.round(f2p * 10),
    f2p_total: 10,
    cost_usd: 1 + run / 10,
  };
}

/**
 * Load the browser's pure row builder without initializing the DOM.
 * @returns {{snapshot:object,buildRows:Function,buildComparisonRows:Function,selectRowsWithinBudget:Function,simulateBaseline:Function,comparePublishedRows:Function,buildBenchmarkSelection:Function}} browser evidence functions
 */
function loadApplication() {
  const context = vm.createContext({
    window: {},
    console,
    Intl,
  });
  vm.runInContext(fs.readFileSync(dataPath, "utf8"), context, {
    filename: dataPath,
  });
  const html = fs.readFileSync(applicationPath, "utf8");
  const match = html.match(
    /<script(?![^>]*\bsrc=)[^>]*>\s*([\s\S]*?)\s*<\/script>/i,
  );
  assert.ok(match, "application inline script was not found");
  const initialization = "initializeApp();";
  const initializationIndex = match[1].lastIndexOf(initialization);
  assert.notEqual(
    initializationIndex,
    -1,
    "application initialization boundary was not found",
  );
  vm.runInContext(
    `${match[1].slice(0, initializationIndex)}
globalThis.__applicationTest = {
  snapshot: SNAPSHOT,
  buildRows,
  buildComparisonRows,
  selectRowsWithinBudget,
  simulateBaseline,
  comparePublishedRows,
  buildBenchmarkSelection,
};`,
    context,
    { filename: applicationPath },
  );
  return context.__applicationTest;
}

test("build-time correlation evidence is deterministic and excludes incomplete cells", () => {
  const rows = [];
  const models = ["weak", "middle", "strong"];
  for (let modelIndex = 0; modelIndex < models.length; modelIndex += 1) {
    for (let run = 0; run < 4; run += 1) {
      rows.push(
        trial(
          "complete-task",
          models[modelIndex],
          run,
          Math.min(1, modelIndex / 2 + run / 20),
          modelIndex / 2,
        ),
      );
      if (!(modelIndex === 1 && run === 3)) {
        rows.push(
          trial(
            "incomplete-task",
            models[modelIndex],
            run,
            Math.min(1, modelIndex / 2 + run / 10),
            modelIndex / 2,
          ),
        );
      }
    }
  }
  const trials = { rows };
  const tasks = {
    rows: [
      {
        id: "complete-task",
        language: "go",
        problem_title: "Complete",
        display_description: "Complete task",
        repository: "fixture/complete",
        repository_url: "https://example.com/complete",
      },
      {
        id: "incomplete-task",
        language: "python",
        problem_title: "Incomplete",
        display_description: "Incomplete task",
        repository: "fixture/incomplete",
        repository_url: "https://example.com/incomplete",
      },
    ],
  };
  const provenance = {
    sourceSha256: "a".repeat(64),
    retrievedAt: "2026-07-30T00:00:00.000Z",
  };

  const first = buildSnapshot(trials, tasks, provenance);
  const second = buildSnapshot(trials, tasks, provenance);

  assert.deepEqual(first, second);
  assert.equal(first.schemaVersion, 3);
  assert.equal(first.correlationMethod.samples, 10_000);
  assert.equal(
    first.calibrationMethod.id,
    "task-ols-inverse-residual-variance-v1",
  );
  assert.equal(
    first.tasks.find((task) => task.id === "complete-task").correlation
      .completeConfigurationCount,
    3,
  );
  assert.equal(
    first.tasks.find((task) => task.id === "incomplete-task").correlation
      .completeConfigurationCount,
    2,
  );
  const completeCalibration = first.tasks.find(
    (task) => task.id === "complete-task",
  ).calibration;
  assert.equal(completeCalibration.configurationCount, 3);
  assert.ok(Number.isFinite(completeCalibration.intercept));
  assert.ok(Number.isFinite(completeCalibration.slope));
  assert.ok(completeCalibration.residualVariance > 0);
  assert.equal(
    completeCalibration.precisionWeight,
    1 / completeCalibration.residualVariance,
  );
  assert.equal(
    first.tasks.find((task) => task.id === "incomplete-task").calibration,
    null,
  );
  for (const task of first.tasks) {
    assert.ok(task.correlation.p05 <= task.correlation.median);
    assert.ok(task.correlation.median <= task.correlation.p95);
  }
});

test("correlation and quantile helpers preserve their mathematical contracts", () => {
  assert.equal(pearsonCorrelation([0, 1, 2], [0, 2, 4]), 1);
  assert.equal(pearsonCorrelation([1, 1, 1], [0, 1, 2]), 0);
  assert.equal(quantile([0, 1, 2, 3, 4], 0.05), 0.2);
  assert.equal(quantile([0, 1, 2, 3, 4], 0.95), 3.8);
});

test("browser rows keep fixed correlations and change only selected-model evidence", () => {
  const application = loadApplication();
  assert.equal(application.snapshot.schemaVersion, 3);
  assert.equal(application.snapshot.correlationMethod.samples, 10_000);

  const firstConfiguration = application.snapshot.configurations[0].id;
  const secondConfiguration = application.snapshot.configurations[1].id;
  const firstRows = application.buildRows(firstConfiguration);
  const secondRows = application.buildRows(secondConfiguration);

  assert.equal(firstRows.length, application.snapshot.tasks.length);
  assert.deepEqual(
    firstRows.map((row) => [row.id, row.correlation]),
    secondRows.map((row) => [row.id, row.correlation]),
  );
  for (let index = 1; index < firstRows.length; index += 1) {
    assert.ok(
      firstRows[index - 1].correlation.p05 >=
        firstRows[index].correlation.p05,
    );
  }
  assert.ok(
    firstRows.some(
      (row, index) => row.cell !== secondRows[index].cell,
    ),
    "selected configuration must change model-specific evidence",
  );
});

test("browser budget selection uses average cost and uniform iterations", () => {
  const { selectRowsWithinBudget } = loadApplication();
  const rows = [
    {
      id: "first",
      cell: { meanCost: 2, mean: 0.7 },
      calibration: { precisionWeight: 1 },
    },
    {
      id: "too-expensive",
      cell: { meanCost: 4, mean: 0.8 },
      calibration: { precisionWeight: 2 },
    },
    {
      id: "later-affordable",
      cell: { meanCost: 0.5, mean: 0.9 },
      calibration: { precisionWeight: 3 },
    },
    { id: "incomplete", cell: null, calibration: null },
  ];

  const selection = selectRowsWithinBudget(rows, 5, 2);

  assert.deepEqual(
    selection.rows.map((row) => row.selected),
    [true, false, true, false],
  );
  assert.equal(selection.selectedCount, 2);
  assert.equal(selection.estimatedSpend, 5);
  assert.equal(selection.rows[0].normalizedWeight, 0.25);
  assert.equal(selection.rows[2].normalizedWeight, 0.75);
});

test("browser headroom excludes saturated tasks before budget selection", () => {
  const { selectRowsWithinBudget } = loadApplication();
  const rows = [
    {
      id: "saturated",
      cell: { meanCost: 1, mean: 0.95 },
      calibration: { precisionWeight: 8 },
    },
    {
      id: "eligible",
      cell: { meanCost: 2, mean: 0.8 },
      calibration: { precisionWeight: 2 },
    },
    {
      id: "also-eligible",
      cell: { meanCost: 3, mean: 0.75 },
      calibration: { precisionWeight: 3 },
    },
  ];

  const selection = selectRowsWithinBudget(rows, 5, 1, 0.1);

  assert.deepEqual(
    selection.rows.map((row) => row.selected),
    [false, true, true],
  );
  assert.equal(selection.selectedCount, 2);
  assert.equal(selection.estimatedSpend, 5);
  assert.equal(selection.rows[0].normalizedWeight, null);
  assert.equal(selection.rows[1].normalizedWeight, 0.4);
  assert.equal(selection.rows[2].normalizedWeight, 0.6);
});

test("comparison rows reuse the primary allocation and reject incomplete cells", () => {
  const { buildComparisonRows } = loadApplication();
  const primaryRows = [
    {
      id: "selected",
      selected: true,
      normalizedWeight: 0.75,
      calibration: { intercept: 0.1, slope: 0.8 },
      cell: { n: 4, meanCost: 1 },
    },
    {
      id: "also-selected",
      selected: true,
      normalizedWeight: 0.25,
      calibration: { intercept: 0.2, slope: 0.5 },
      cell: { n: 4, meanCost: 2 },
    },
    {
      id: "not-selected",
      selected: false,
      normalizedWeight: null,
      calibration: { intercept: 0.3, slope: 0.4 },
      cell: { n: 4, meanCost: 3 },
    },
  ];
  const tasks = [
    {
      id: "selected",
      cells: { comparison: { n: 4, meanCost: 4 } },
    },
    {
      id: "also-selected",
      cells: { comparison: { n: 4, meanCost: 5 } },
    },
    {
      id: "not-selected",
      cells: { comparison: { n: 4, meanCost: 6 } },
    },
  ];

  const comparison = buildComparisonRows(
    primaryRows,
    "comparison",
    tasks,
  );

  assert.deepEqual(
    comparison.rows.map((row) => row.id),
    ["selected", "also-selected"],
  );
  assert.deepEqual(
    comparison.rows.map((row) => row.normalizedWeight),
    [0.75, 0.25],
  );
  assert.deepEqual(
    comparison.rows.map((row) => row.cell.meanCost),
    [4, 5],
  );
  assert.equal(comparison.missingTaskIds.length, 0);

  tasks[1].cells.comparison.n = 3;
  const incomplete = buildComparisonRows(
    primaryRows,
    "comparison",
    tasks,
  );
  assert.equal(incomplete.rows, null);
  assert.equal(incomplete.missingTaskIds.length, 1);
  assert.equal(incomplete.missingTaskIds[0], "also-selected");
});

test("baseline simulation is deterministic and returns score and price ranges", () => {
  const { simulateBaseline } = loadApplication();
  const rows = [
    {
      id: "first",
      selected: true,
      normalizedWeight: 0.25,
      calibration: { intercept: 0.1, slope: 0.8 },
      cell: {
        trials: [
          { score: 0.2, cost: 1 },
          { score: 0.4, cost: 2 },
          { score: 0.6, cost: 3 },
          { score: 0.8, cost: 4 },
        ],
      },
    },
    {
      id: "second",
      selected: true,
      normalizedWeight: 0.75,
      calibration: { intercept: 0.2, slope: 0.5 },
      cell: {
        trials: [
          { score: 0.1, cost: 2 },
          { score: 0.3, cost: 3 },
          { score: 0.5, cost: 4 },
          { score: 0.7, cost: 5 },
        ],
      },
    },
  ];

  const first = simulateBaseline(rows, 2, "deterministic fixture");
  const second = simulateBaseline(rows, 2, "deterministic fixture");

  assert.equal(JSON.stringify(first), JSON.stringify(second));
  assert.ok(first.score.p05 <= first.score.mean);
  assert.ok(first.score.mean <= first.score.p95);
  assert.ok(first.price.p05 <= first.price.mean);
  assert.ok(first.price.mean <= first.price.p95);

  const unbounded = simulateBaseline(
    [
      {
        id: "unbounded",
        selected: true,
        normalizedWeight: 1,
        calibration: { intercept: -0.5, slope: 0 },
        cell: {
          trials: [
            { score: 0, cost: 1 },
            { score: 0, cost: 1 },
            { score: 0, cost: 1 },
            { score: 0, cost: 1 },
          ],
        },
      },
    ],
    1,
    "unbounded fixture",
  );
  assert.equal(unbounded.score.mean, -0.5);
  assert.equal(unbounded.score.p05, -0.5);
  assert.equal(unbounded.score.p95, -0.5);
});

test("published comparison uses the fixed weighted Welch calculation", () => {
  const { comparePublishedRows } = loadApplication();
  const primary = [
    {
      id: "task",
      selected: true,
      normalizedWeight: 1,
      calibration: { slope: 1 },
      cell: { n: 4, mean: 0.8, variance: 0.04 },
    },
  ];
  const comparison = [
    {
      ...primary[0],
      cell: { n: 4, mean: 0.5, variance: 0.01 },
    },
  ];

  const result = comparePublishedRows(primary, comparison, 2);

  assert.ok(Math.abs(result.difference - 0.3) < 1e-12);
  assert.ok(Math.abs(result.standardError - Math.sqrt(0.025)) < 1e-12);
  assert.ok(Math.abs(result.degreesOfFreedom - 4.411764705882353) < 1e-12);
  assert.ok(result.pValue > 0 && result.pValue < 1);
  assert.equal(
    comparePublishedRows(
      [{ ...primary[0], cell: { n: 4, mean: 1, variance: 0 } }],
      [{ ...comparison[0], cell: { n: 4, mean: 1, variance: 0 } }],
      1,
    ),
    null,
  );
});

test("copied selection matches the benchmark CLI schema", () => {
  const { snapshot, buildBenchmarkSelection } = loadApplication();
  const configuration = snapshot.configurations[0];
  const selection = {
    estimatedSpend: 2.5,
    rows: [
      {
        id: "selected-task",
        selected: true,
        normalizedWeight: 1,
        calibration: { intercept: 0.1, slope: 0.8 },
        cell: { meanCost: 1.25 },
      },
      {
        id: "not-selected",
        selected: false,
        normalizedWeight: null,
        calibration: { intercept: 0.2, slope: 0.5 },
        cell: { meanCost: 1 },
      },
    ],
  };

  const document = buildBenchmarkSelection(
    selection,
    configuration.id,
    5,
    2,
  );

  assert.equal(document.schema, "deepswe-benchmark-selection");
  assert.equal(document.schemaVersion, 1);
  assert.equal(document.snapshot.sha256, snapshot.source.sha256);
  assert.deepEqual(
    [document.selector.model, document.selector.reasoning],
    [configuration.model, configuration.reasoning],
  );
  assert.equal(document.estimatedPublishedSpendUsd, 2.5);
  assert.equal(
    JSON.stringify(document.tasks),
    JSON.stringify([
      {
        id: "selected-task",
        repetitions: 2,
        weight: 1,
        calibration: { intercept: 0.1, slope: 0.8 },
        publishedMeanCostUsd: 1.25,
      },
    ]),
  );
});

test("matrix selection identity matches the Go canonical identity", () => {
  const selection = {
    schema: "deepswe-benchmark-selection",
    schemaVersion: 1,
    snapshot: {
      url: "https://deepswe.datacurve.ai/artifacts/v1.1/trials.json",
      sha256: "a".repeat(64),
    },
    selector: {
      model: "gpt-5-6-luna",
      reasoning: "low",
      budgetUsd: 0.2,
      iterationsPerTask: 1,
    },
    estimatedPublishedSpendUsd: 0.2,
    tasks: [
      {
        id: "first-task",
        repetitions: 1,
        weight: 0.25,
        calibration: { intercept: 0.1, slope: 0.8 },
        publishedMeanCostUsd: 0.1,
      },
      {
        id: "second-task",
        repetitions: 1,
        weight: 0.75,
        calibration: { intercept: 0.2, slope: 0.5 },
        publishedMeanCostUsd: 0.1,
      },
    ],
  };

  assert.equal(
    matrixSelectionID(selection),
    "62ab8876c8a70c14c3994401243eece6e6e807aa39629e42952b84adaa5bc8b0",
  );
  selection.tasks[0].calibration.slope = 0.7;
  assert.notEqual(
    matrixSelectionID(selection),
    "62ab8876c8a70c14c3994401243eece6e6e807aa39629e42952b84adaa5bc8b0",
  );
});
