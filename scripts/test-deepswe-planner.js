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
 * @returns {{snapshot:object,optimize:Function,suiteStatistics:Function,deriveTask:Function,buildExport:Function}} planner functions
 */
function loadPlanner() {
  const context = vm.createContext({ window: {}, console });
  vm.runInContext(fs.readFileSync(dataPath, "utf8"), context, {
    filename: dataPath,
  });
  const html = fs.readFileSync(applicationPath, "utf8");
  const match = html.match(/<script>\n([\s\S]*?)\n<\/script>/);
  assert.ok(match, "planner inline script was not found");
  const initialization = "\ninitializeSelectors();";
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
};`,
    context,
    { filename: applicationPath },
  );
  return context.__plannerTest;
}

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
