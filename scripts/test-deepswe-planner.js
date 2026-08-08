#!/usr/bin/env node
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

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
 * Load the planner's actual browser calculation functions without running its
 * DOM initialization.
 * @returns {{snapshot:object,optimize:Function,suiteStatistics:Function,deriveTask:Function,buildExport:Function,isExportablePlan:Function}} planner functions
 */
function loadPlanner() {
  const context = vm.createContext({ window: {}, console });
  vm.runInContext(fs.readFileSync(dataPath, "utf8"), context, {
    filename: dataPath,
  });
  const html = fs.readFileSync(applicationPath, "utf8");
  const match = html.match(/<script(?![^>]*\bsrc=)[^>]*>\s*([\s\S]*?)\s*<\/script>/i);
  assert.ok(match, "planner inline script was not found");
  const initialization = "initializeSelectors();";
  const initializationIndex = match[1].lastIndexOf(initialization);
  assert.notEqual(
    initializationIndex,
    -1,
    "planner DOM initialization boundary was not found",
  );
  const calculationSource = match[1].slice(0, initializationIndex);
  vm.runInContext(
    `${calculationSource}
globalThis.__plannerTest = {
  snapshot: SNAPSHOT,
  optimize,
  suiteStatistics,
  deriveTask,
  buildExport,
  isExportablePlan,
};`,
    context,
    { filename: applicationPath },
  );
  return context.__plannerTest;
}

test("planner explains its purpose, model availability, and evidence source", () => {
  const html = fs.readFileSync(applicationPath, "utf8");
  assert.match(html, /id="overview-title">What this planner does</);
  assert.match(html, /arXiv paper forthcoming/);
  assert.match(html, /family=Inter:wght@400;500;600;700&family=JetBrains\+Mono/);
  assert.match(html, /src="\.\/agent-layer-logo\.svg"/);
  assert.match(html, /<strong>Required · Runnable models only\.<\/strong>/);
  assert.match(html, /<strong>Optional · Plot only\.<\/strong>/);
  assert.match(html, /<option value="">No comparison<\/option>/);
  assert.match(html, /id="estimateChart"/);
  assert.match(html, /data-task-excluded=/);
  assert.doesNotMatch(html, /data-task-state=/);
  assert.doesNotMatch(html, />Required<\/option>/);
  assert.match(html, /<th>Runs in current plan<\/th>/);
  assert.match(html, /<th>Evidence<\/th>/);
  assert.doesNotMatch(html, /<th>Comparison score<\/th>/);
  assert.doesNotMatch(html, /<th>Historical difference<\/th>/);
  assert.match(html, /<section class="card data-card"/);
  assert.ok(
    fs.existsSync(path.join(path.dirname(applicationPath), "agent-layer-logo.svg")),
    "planner uses a local copy of the real Agent Layer logo",
  );
  assert.doesNotMatch(
    html,
    /<header class="app-header">[\s\S]*?id="provenance"[\s\S]*?<\/header>/,
  );
});

test("comparison is optional and cannot change the recommended plan", () => {
  const planner = loadPlanner();
  assert.equal(planner.snapshot.schemaVersion, 3);
  assert.equal(planner.snapshot.correlationMethod.samples, 10_000);
  const baseInputs = {
    targetId: "gpt-5-6-luna::low",
    comparisonId: "",
    budget: 2,
    minTasks: 3,
    maxReps: 3,
    headroom: 0.1,
  };
  const comparisonInputs = {
    ...baseInputs,
    comparisonId: "gpt-5-6-luna::medium",
  };
  const baseRows = planner.snapshot.tasks.map((task) =>
    planner.deriveTask(task, baseInputs),
  );
  const comparisonRows = planner.snapshot.tasks.map((task) =>
    planner.deriveTask(task, comparisonInputs),
  );
  const basePlan = planner.optimize(baseInputs, baseRows);
  const comparisonPlan = planner.optimize(comparisonInputs, comparisonRows);
  const allocation = (plan) => plan.selections.map((selection) => [
    selection.row.id,
    selection.repetitions,
  ]);
  assert.deepEqual(allocation(basePlan), allocation(comparisonPlan));
  assert.equal(basePlan.statistics.detectability, comparisonPlan.statistics.detectability);
  const exported = JSON.parse(JSON.stringify(planner.buildExport(basePlan, baseInputs)));
  assert.equal("comparison" in exported, false);
  assert.ok(exported.tasks.every((task) => !("comparison" in task)));
});

/**
 * Exhaustively enumerate every allowed task/repetition allocation.
 * @param {object[]} rows eligible planner rows
 * @param {object} inputs planner inputs
 * @param {Function} suiteStatistics production statistics function
 * @returns {{selections:object[],statistics:object,spent:number}} exact optimum
 */
function exhaustiveOptimum(rows, inputs, suiteStatistics) {
  const choices = [0];
  for (let repetitions = 2; repetitions <= inputs.maxReps; repetitions += 1) {
    choices.push(repetitions);
  }
  let best = null;
  const selections = [];

  /**
   * Visit one allocation prefix.
   * @param {number} index next row index
   * @param {number} spent accumulated baseline cost
   * @returns {void}
   */
  function visit(index, spent) {
    if (spent > inputs.budget + 1e-12) return;
    if (index === rows.length) {
      if (selections.length < inputs.minTasks) return;
      const statistics = suiteStatistics(selections, true);
      if (
        !best ||
        statistics.detectability > best.statistics.detectability + 1e-12 ||
        (Math.abs(statistics.detectability - best.statistics.detectability) <=
          1e-12 &&
          spent < best.spent)
      ) {
        best = {
          selections: selections.map((selection) => ({ ...selection })),
          statistics,
          spent,
        };
      }
      return;
    }
    for (const repetitions of choices) {
      if (repetitions > 0) {
        selections.push({ row: rows[index], repetitions });
      }
      visit(index + 1, spent + rows[index].cost * repetitions);
      if (repetitions > 0) selections.pop();
    }
  }

  visit(0, 0);
  assert.ok(best, "fixture must contain an affordable allocation");
  return best;
}

test("planner search matches exhaustive allocation and exports stable JSON", () => {
  const planner = loadPlanner();
  const inputs = {
    targetId: "gpt-5-6-luna::low",
    comparisonId: "gpt-5-6-luna::medium",
    budget: 0,
    minTasks: 3,
    maxReps: 4,
    headroom: 0.1,
  };
  const rows = planner.snapshot.tasks
    .map((task) => planner.deriveTask(task, inputs))
    .filter((row) => row.automaticEligible)
    .sort((left, right) => left.id.localeCompare(right.id))
    .slice(0, 6);
  assert.equal(rows.length, 6, "fixture requires six eligible published tasks");

  const minimumBreadthCost = [...rows]
    .sort((left, right) => left.cost - right.cost)
    .slice(0, inputs.minTasks)
    .reduce((sum, row) => sum + row.cost * 2, 0);
  inputs.budget = minimumBreadthCost + 0.75;

  const exact = exhaustiveOptimum(
    rows,
    inputs,
    planner.suiteStatistics,
  );
  const actual = planner.optimize(inputs, rows);

  assert.equal(actual.valid, true);
  assert.equal(actual.optimization.certified, true);
  assert.ok(actual.spent <= inputs.budget + 1e-12);
  assert.ok(actual.selections.length >= inputs.minTasks);
  for (const selection of actual.selections) {
    assert.ok(selection.repetitions >= 2 && selection.repetitions <= 4);
  }
  assert.ok(
    Math.abs(
      actual.statistics.detectability - exact.statistics.detectability,
    ) <= 1e-12,
    `detectability ${actual.statistics.detectability} != exhaustive ${exact.statistics.detectability}`,
  );
  const firstExport = JSON.stringify(planner.buildExport(actual, inputs));
  const secondExport = JSON.stringify(planner.buildExport(actual, inputs));
  assert.equal(firstExport, secondExport);
  const exported = JSON.parse(firstExport);
  assert.equal(exported.schema, "deepswe-benchmark-plan");
  assert.equal("generatedAt" in exported, false);
  assert.equal(exported.costAxis.valid, true);
  assert.equal(
    exported.costAxis.referenceConfiguration,
    "claude-fable-5::max",
  );
  assert.deepEqual(exported.costAxis.missingTasks, []);
});

test("tasks without report cost-axis evidence cannot enter a paid plan", () => {
  const planner = loadPlanner();
  const inputs = {
    targetId: "gpt-5-6-luna::low",
    comparisonId: "gpt-5-6-luna::medium",
    budget: 1,
    minTasks: 1,
    maxReps: 4,
    headroom: 0.1,
  };
  const task = JSON.parse(JSON.stringify(planner.snapshot.tasks[0]));
  delete task.cells["claude-fable-5::max"];
  const row = planner.deriveTask(task, inputs);
  assert.equal(row.automaticEligible, false);
  assert.ok(
    row.reasons.some((reason) =>
      reason.includes("claude-fable-5::max cost-axis reference"),
    ),
  );
});

test("invalid best-effort plans cannot be exported", () => {
  const planner = loadPlanner();
  assert.equal(planner.isExportablePlan({ valid: false, selections: [{}] }), false);
  assert.equal(planner.isExportablePlan({ valid: true, selections: [] }), false);
  assert.equal(planner.isExportablePlan({ valid: true, selections: [{}] }), true);
});
