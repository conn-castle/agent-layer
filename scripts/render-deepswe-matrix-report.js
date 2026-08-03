#!/usr/bin/env node
"use strict";

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

function usage() {
  return [
    "Usage:",
    "  node scripts/render-deepswe-matrix-report.js \\",
    "    --selection PATH --trials PATH --matrix-dir PATH \\",
    "    --output PATH --json-output PATH \\",
    "    [--additional-baselines-from MATRIX_DIR] \\",
    "    [--additional-arms-from MATRIX_DIR]",
  ].join("\n");
}

function parseArguments(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index];
    const value = argv[index + 1];
    if (!flag?.startsWith("--") || !value || value.startsWith("--")) {
      throw new Error(usage());
    }
    const key = flag.slice(2);
    if (values[key]) throw new Error(`duplicate --${key}\n${usage()}`);
    values[key] = value;
  }
  const required = ["selection", "trials", "matrix-dir", "output", "json-output"];
  const optional = ["additional-baselines-from", "additional-arms-from"];
  const unknown = Object.keys(values).filter(
    (key) => !required.includes(key) && !optional.includes(key),
  );
  const missing = required.filter((key) => !values[key]);
  if (unknown.length || missing.length) {
    throw new Error(
      `${unknown.length ? `unknown flags: ${unknown.join(", ")}\n` : ""}${
        missing.length ? `missing flags: ${missing.join(", ")}\n` : ""
      }${usage()}`,
    );
  }
  return values;
}

function readJson(filePath, description) {
  let contents;
  try {
    contents = fs.readFileSync(filePath, "utf8");
  } catch (error) {
    throw new Error(`cannot read ${description} ${filePath}: ${error.message}`);
  }
  try {
    return JSON.parse(contents);
  } catch (error) {
    throw new Error(`cannot parse ${description} ${filePath}: ${error.message}`);
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function sha256File(filePath) {
  return crypto.createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
}

function sampleVariance(values) {
  assert(values.length > 1, "sample variance requires at least two values");
  const mean = values.reduce((sum, value) => sum + value, 0) / values.length;
  return (
    values.reduce((sum, value) => sum + (value - mean) ** 2, 0) /
    (values.length - 1)
  );
}

function validateSelection(selection) {
  assert(
    selection.schema === "deepswe-benchmark-selection" && selection.schemaVersion === 1,
    "unsupported benchmark selection schema",
  );
  assert(/^[a-f0-9]{64}$/.test(selection.snapshot?.sha256 ?? ""), "selection lacks a valid snapshot hash");
  assert(Array.isArray(selection.tasks) && selection.tasks.length > 0, "selection has no tasks");
  const seen = new Set();
  let weight = 0;
  for (const task of selection.tasks) {
    assert(typeof task.id === "string" && task.id, "selection contains an invalid task ID");
    assert(!seen.has(task.id), `selection repeats task ${task.id}`);
    seen.add(task.id);
    assert(Number.isInteger(task.repetitions) && task.repetitions > 0, `${task.id} has invalid repetitions`);
    assert(Number.isFinite(task.weight) && task.weight > 0, `${task.id} has invalid weight`);
    assert(Number.isFinite(task.calibration?.intercept), `${task.id} has invalid calibration intercept`);
    assert(Number.isFinite(task.calibration?.slope), `${task.id} has invalid calibration slope`);
    weight += task.weight;
  }
  assert(Math.abs(weight - 1) < 1e-9, `selection weights sum to ${weight}, expected 1`);
}

function matrixSelectionID(selection) {
  const canonical = {
    schema: selection.schema,
    schemaVersion: selection.schemaVersion,
    snapshot: {
      url: selection.snapshot.url,
      sha256: selection.snapshot.sha256,
    },
    selector: {
      model: selection.selector.model,
      reasoning: selection.selector.reasoning,
      budgetUsd: selection.selector.budgetUsd,
      iterationsPerTask: selection.selector.iterationsPerTask,
    },
    estimatedPublishedSpendUsd: selection.estimatedPublishedSpendUsd,
    tasks: selection.tasks.map((task) => ({
      id: task.id,
      repetitions: task.repetitions,
      weight: task.weight,
      calibration: {
        intercept: task.calibration.intercept,
        slope: task.calibration.slope,
      },
      publishedMeanCostUsd: task.publishedMeanCostUsd,
    })),
  };
  return crypto
    .createHash("sha256")
    .update(JSON.stringify(canonical))
    .digest("hex");
}

const REASONING_EFFORT_ORDER = new Map([
  ["minimal", 0],
  ["low", 1],
  ["medium", 2],
  ["high", 3],
  ["xhigh", 4],
  ["max", 5],
]);

/**
 * Order report arms deterministically: baselines first, then by model, then by
 * increasing reasoning effort, then by creation time and label.
 *
 * @param {Array<object>} arms discovered arms, sorted in place
 * @returns {Array<object>} the same array, sorted
 */
function sortArmsForReport(arms) {
  return arms.sort(
    (left, right) =>
      (left.mode === "baseline" ? 0 : 1) - (right.mode === "baseline" ? 0 : 1) ||
      left.model.localeCompare(right.model) ||
      (REASONING_EFFORT_ORDER.get(left.reasoning) ?? 99) -
        (REASONING_EFFORT_ORDER.get(right.reasoning) ?? 99) ||
      left.createdAt.localeCompare(right.createdAt) ||
      left.label.localeCompare(right.label),
  );
}

function discoverCompletedArms(matrixDir, selection) {
  const armsDir = path.join(matrixDir, "arms");
  const entries = fs
    .readdirSync(armsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .sort((left, right) => left.name.localeCompare(right.name));
  const arms = [];
  const skipped = [];
  for (const entry of entries) {
    const armDir = path.join(armsDir, entry.name);
    const manifestPath = path.join(armDir, "manifest.json");
    if (!fs.existsSync(manifestPath)) {
      skipped.push(`${entry.name}: missing manifest`);
      continue;
    }
    const manifest = readJson(manifestPath, "matrix arm manifest");
    assert(manifest.selection_id === path.basename(matrixDir), `${entry.name}: selection identity mismatch`);
    assert(manifest.mode === "baseline" || manifest.mode === "treatment", `${entry.name}: invalid mode`);
    assert(typeof manifest.label === "string" && manifest.label, `${entry.name}: missing label`);
    const taskReports = [];
    let complete = true;
    // Arms written before the certified-environment harness carry no identity at
    // all. Legacy manifests recorded one identity map per arm; current results
    // record the identity per attempt so a single task can be re-certified.
    let resultsCarryEnvironmentIdentity = true;
    for (const task of selection.tasks) {
      const scores = [];
      const cost = { midpoint: 0, minimum: 0, maximum: 0 };
      let verifierBuildFailed = false;
      let providerClientVersion = "";
      let invocationCount = 0;
      let dispatchConformantRuns = 0;
      for (let attempt = 1; attempt <= task.repetitions; attempt += 1) {
        const resultPath = path.join(
          armDir,
          "attempts",
          String(attempt),
          "tasks",
          task.id,
          "result.json",
        );
        if (!fs.existsSync(resultPath)) {
          complete = false;
          break;
        }
        const result = readJson(resultPath, "matrix result");
        assert(result.task === task.id && result.attempt === attempt, `${entry.name}: mismatched ${task.id} result`);
        assert(result.status === "success", `${entry.name}: ${task.id} attempt ${attempt} is not successful`);
        assert(Number.isFinite(result.f2p_score), `${entry.name}: ${task.id} has invalid score`);
        assert(Number.isFinite(result.cost_usd), `${entry.name}: ${task.id} has invalid cost`);
        assert(result.reasoning_effort === manifest.reasoning, `${entry.name}: ${task.id} reasoning mismatch`);
        if (providerClientVersion && providerClientVersion !== result.provider_client_version) {
          throw new Error(`${entry.name}: mixed provider client versions`);
        }
        providerClientVersion = result.provider_client_version;
        scores.push(result.f2p_score);
        cost.midpoint += result.cost_usd;
        cost.minimum += result.cost_min_usd ?? result.cost_usd;
        cost.maximum += result.cost_max_usd ?? result.cost_usd;
        verifierBuildFailed ||= Boolean(result.verifier_build_failed);
        invocationCount += result.invocation_count;
        dispatchConformantRuns += result.dispatch_conformant ? 1 : 0;
        resultsCarryEnvironmentIdentity &&= Boolean(result.task_environment_identity);
      }
      if (!complete) break;
      const f2pScore = scores.reduce((sum, score) => sum + score, 0) / scores.length;
      const calibratedScore = task.calibration.intercept + task.calibration.slope * f2pScore;
      taskReports.push({
        task: task.id,
        repetitions: task.repetitions,
        f2pScore,
        calibratedScore,
        weightedContribution: task.weight * calibratedScore,
        weight: task.weight,
        cost,
        verifierBuildFailed,
        providerClientVersion,
        invocationCount,
        dispatchConformantRuns,
      });
    }
    if (!complete) {
      skipped.push(`${manifest.label}: incomplete`);
      continue;
    }
    const cost = taskReports.reduce(
      (total, task) => ({
        midpoint: total.midpoint + task.cost.midpoint,
        minimum: total.minimum + task.cost.minimum,
        maximum: total.maximum + task.cost.maximum,
      }),
      { midpoint: 0, minimum: 0, maximum: 0 },
    );
    const providerClients = new Set(taskReports.map((task) => task.providerClientVersion));
    assert(providerClients.size === 1, `${manifest.label}: mixed provider client versions`);
    arms.push({
      id: entry.name,
      label: manifest.label,
      mode: manifest.mode,
      model: manifest.model,
      reasoning: manifest.reasoning,
      createdAt: manifest.created_at,
      environmentQualified:
        Boolean(manifest.task_environment_identities) ||
        resultsCarryEnvironmentIdentity,
      providerClientVersion: [...providerClients][0],
      score: taskReports.reduce((sum, task) => sum + task.weightedContribution, 0),
      cost,
      invocationCount: taskReports.reduce((sum, task) => sum + task.invocationCount, 0),
      dispatchConformantRuns: taskReports.reduce(
        (sum, task) => sum + task.dispatchConformantRuns,
        0,
      ),
      verifierBuildFailedRuns: taskReports.filter((task) => task.verifierBuildFailed).length,
      tasks: taskReports,
    });
  }
  const duplicateLabels = new Map();
  for (const arm of arms) {
    duplicateLabels.set(arm.label, (duplicateLabels.get(arm.label) ?? 0) + 1);
  }
  for (const arm of arms) {
    if (duplicateLabels.get(arm.label) > 1) {
      const harness = arm.environmentQualified ? "certified harness" : "legacy harness";
      arm.label = `${arm.label} (${harness})`;
    }
  }
  const resolvedLabels = new Set();
  for (const arm of arms) {
    if (resolvedLabels.has(arm.label)) {
      arm.label = `${arm.label} [${arm.id.slice(0, 8)}]`;
    }
    resolvedLabels.add(arm.label);
  }
  sortArmsForReport(arms);
  assert(arms.length >= 2, "fewer than two completed matrix arms were discovered");
  return { arms, skipped };
}

function latestBaselinePerConfiguration(arms) {
  const latest = new Map();
  for (const arm of arms.filter((candidate) => candidate.mode === "baseline")) {
    const key = `${arm.model}\u0000${arm.reasoning}`;
    const existing = latest.get(key);
    if (!existing || existing.createdAt.localeCompare(arm.createdAt) < 0) {
      latest.set(key, arm);
    }
  }
  return [...latest.values()].map((arm) => ({
    ...arm,
    label: arm.label.replace(/ \((?:certified|legacy) harness\)$/, ""),
  }));
}

function publishedCell(trials, task, arm) {
  const values = trials
    .filter(
      (trial) =>
        trial.model === arm.model &&
        trial.reasoning_effort === arm.reasoning &&
        trial.task_name === task &&
        typeof trial.f2p === "number" &&
        !trial.errored,
    )
    .map((trial) => trial.f2p);
  assert(
    values.length === 4,
    `${arm.label}: ${task} has ${values.length} complete published trials, expected 4`,
  );
  return { variance: sampleVariance(values), degreesOfFreedom: values.length - 1 };
}

function logGamma(value) {
  const coefficients = [
    76.18009172947146,
    -86.50532032941677,
    24.01409824083091,
    -1.231739572450155,
    0.001208650973866179,
    -0.000005395239384953,
  ];
  let cursor = value;
  let temporary = value + 5.5;
  temporary -= (value + 0.5) * Math.log(temporary);
  let series = 1.000000000190015;
  for (const coefficient of coefficients) series += coefficient / ++cursor;
  return -temporary + Math.log((2.5066282746310005 * series) / value);
}

function betaFraction(a, b, x) {
  const floor = 1e-300;
  const qab = a + b;
  const qap = a + 1;
  const qam = a - 1;
  let c = 1;
  let d = 1 - (qab * x) / qap;
  if (Math.abs(d) < floor) d = floor;
  d = 1 / d;
  let result = d;
  for (let iteration = 1; iteration <= 200; iteration += 1) {
    const doubled = 2 * iteration;
    let coefficient =
      (iteration * (b - iteration) * x) /
      ((qam + doubled) * (a + doubled));
    d = 1 + coefficient * d;
    if (Math.abs(d) < floor) d = floor;
    c = 1 + coefficient / c;
    if (Math.abs(c) < floor) c = floor;
    d = 1 / d;
    result *= d * c;
    coefficient =
      (-(a + iteration) * (qab + iteration) * x) /
      ((a + doubled) * (qap + doubled));
    d = 1 + coefficient * d;
    if (Math.abs(d) < floor) d = floor;
    c = 1 + coefficient / c;
    if (Math.abs(c) < floor) c = floor;
    d = 1 / d;
    const delta = d * c;
    result *= delta;
    if (Math.abs(delta - 1) < 3e-14) break;
  }
  return result;
}

function regularizedBeta(a, b, x) {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  const factor = Math.exp(
    logGamma(a + b) -
      logGamma(a) -
      logGamma(b) +
      a * Math.log(x) +
      b * Math.log(1 - x),
  );
  return x < (a + 1) / (a + b + 2)
    ? (factor * betaFraction(a, b, x)) / a
    : 1 - (factor * betaFraction(b, a, 1 - x)) / b;
}

function tTail(value, degreesOfFreedom) {
  return regularizedBeta(
    degreesOfFreedom / 2,
    0.5,
    degreesOfFreedom / (degreesOfFreedom + value * value),
  );
}

function compareArms(selection, trials, left, right) {
  let variance = 0;
  let welchDenominator = 0;
  for (const task of selection.tasks) {
    const coefficient = task.weight * task.calibration.slope;
    for (const [arm, cell] of [
      [left, publishedCell(trials, task.id, left)],
      [right, publishedCell(trials, task.id, right)],
    ]) {
      const component =
        (coefficient ** 2 * cell.variance) / task.repetitions;
      variance += component;
      welchDenominator += component ** 2 / cell.degreesOfFreedom;
      assert(arm.model, "comparison arm lacks a model");
    }
  }
  assert(variance > 0 && welchDenominator > 0, `${left.label} vs ${right.label}: variance is not estimable`);
  const difference = left.score - right.score;
  const standardError = Math.sqrt(variance);
  const degreesOfFreedom = variance ** 2 / welchDenominator;
  const statistic = Math.abs(difference) / standardError;
  return {
    difference,
    standardError,
    degreesOfFreedom,
    statistic,
    pValue: tTail(statistic, degreesOfFreedom),
  };
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function formatPValue(value) {
  if (value === null) return "—";
  return value < 0.0001 ? value.toExponential(2) : value.toFixed(4);
}

function renderHtml(report) {
  const colors = [
    "#64748b",
    "#2f855a",
    "#2563a5",
    "#d97706",
    "#7c3aed",
    "#be185d",
    "#0891b2",
    "#65a30d",
    "#c2410c",
    "#4338ca",
    "#0f766e",
    "#a16207",
  ];
  const shortLabel = (arm) => `${arm.mode === "treatment" ? "AL" : "Bare"} ${arm.reasoning}`;
  const summaryRows = report.arms
    .map(
      (arm, index) => `<tr><th class="run"><span class="swatch" style="background:${colors[index % colors.length]}"></span><strong>${escapeHtml(arm.label)}</strong><small>${escapeHtml(arm.mode)}</small></th><td class="reasoning">${escapeHtml(arm.reasoning)}</td><td class="score">${(arm.score * 100).toFixed(2)}%</td><td class="cost">$${arm.cost.midpoint.toFixed(2)}</td><td class="cost range">$${arm.cost.minimum.toFixed(2)}–$${arm.cost.maximum.toFixed(2)}</td><td class="number">${arm.invocationCount}</td></tr>`,
    )
    .join("");
  const pHeaders = report.arms
    .map((arm) => `<th class="phead">vs ${escapeHtml(shortLabel(arm))}</th>`)
    .join("");
  const pRows = report.arms
    .map((arm, rowIndex) => {
      const cells = report.pValueMatrix[rowIndex]
        .map((comparison, columnIndex) => {
          if (comparison === null) return '<td class="pvalue diagonal">—</td>';
          const significant = comparison.pValue < 0.05 ? " significant" : "";
          return `<td class="pvalue${significant}" title="Row minus column: ${(arm.score - report.arms[columnIndex].score) * 100} points; t=${comparison.statistic.toFixed(3)}; df=${comparison.degreesOfFreedom.toFixed(2)}">${formatPValue(comparison.pValue)}</td>`;
        })
        .join("");
      return `<tr><th class="run"><span class="swatch" style="background:${colors[rowIndex % colors.length]}"></span><strong>${escapeHtml(arm.label)}</strong></th>${cells}</tr>`;
    })
    .join("");

  const width = 1280;
  const height = 500;
  const margin = { top: 36, right: 340, bottom: 76, left: 76 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const observedMaximum = Math.max(...report.arms.map((arm) => arm.cost.maximum));
  const maximumCost = Math.max(1, Math.ceil(observedMaximum / 25) * 25);
  const x = (value) =>
    margin.left +
    (Math.log10(1 + Math.max(0, value)) / Math.log10(1 + maximumCost)) * plotWidth;
  const y = (value) => margin.top + (1 - value) * plotHeight;
  const scoreGrid = [0, 0.25, 0.5, 0.75, 1]
    .map((value) => `<line class="grid" x1="${margin.left}" y1="${y(value)}" x2="${width - margin.right}" y2="${y(value)}"></line><text class="tick" x="${margin.left - 12}" y="${y(value) + 4}" text-anchor="end">${value * 100}%</text>`)
    .join("");
  const candidateTicks = [0, 1, 5, 10, 25, 50, 75, 100, maximumCost];
  const costTicks = [...new Set(candidateTicks.filter((value) => value <= maximumCost))].sort((a, b) => a - b);
  const costGrid = costTicks
    .map((value) => `<line class="grid" x1="${x(value)}" y1="${margin.top}" x2="${x(value)}" y2="${height - margin.bottom}"></line><text class="tick" x="${x(value)}" y="${height - margin.bottom + 24}" text-anchor="middle">$${value}</text>`)
    .join("");
  let chartPoints = report.arms
    .map((arm, index) => {
      const pointX = x(arm.cost.midpoint);
      const pointY = y(arm.score);
      const labelRight = pointX < width * 0.72;
      const labelX = pointX + (labelRight ? 12 : -12);
      const labelY = pointY + (index % 2 === 0 ? -13 : 25);
      const anchor = labelRight ? "start" : "end";
      const color = colors[index % colors.length];
      return `<g><line class="cost-range" x1="${x(arm.cost.minimum)}" y1="${pointY}" x2="${x(arm.cost.maximum)}" y2="${pointY}" stroke="${color}"></line><line class="cost-cap" x1="${x(arm.cost.minimum)}" y1="${pointY - 6}" x2="${x(arm.cost.minimum)}" y2="${pointY + 6}" stroke="${color}"></line><line class="cost-cap" x1="${x(arm.cost.maximum)}" y1="${pointY - 6}" x2="${x(arm.cost.maximum)}" y2="${pointY + 6}" stroke="${color}"></line><circle class="point" cx="${pointX}" cy="${pointY}" r="8" fill="${color}"></circle><text class="point-label" x="${labelX}" y="${labelY}" text-anchor="${anchor}">${escapeHtml(arm.label)}</text><text class="point-value" x="${labelX}" y="${labelY + 16}" text-anchor="${anchor}">${(arm.score * 100).toFixed(2)}% · $${arm.cost.midpoint.toFixed(2)}</text></g>`;
    })
    .join("");
  let pointIndex = 0;
  chartPoints = chartPoints.replace(/<g>/g, () => {
    const arm = report.arms[pointIndex++];
    return (
      '<g tabindex="0" style="cursor:help"><title>' +
      escapeHtml(arm.label) +
      ': ' +
      (arm.score * 100).toFixed(2) +
      '% · $' +
      arm.cost.midpoint.toFixed(2) +
      ' midpoint cost; range $' +
      arm.cost.minimum.toFixed(2) +
      '–$' +
      arm.cost.maximum.toFixed(2) +
      '</title>'
    );
  });
  chartPoints = chartPoints.replace(
    /<text class="point-label"[^>]*>.*?<\/text><text class="point-value"[^>]*>.*?<\/text>/g,
    "",
  );
  chartPoints +=
    '<line x1="' +
    (width - margin.right) +
    '" y1="36" x2="' +
    (width - margin.right) +
    '" y2="' +
    (height - margin.bottom) +
    '" stroke="#d9ded9"></line><text x="' +
    (width - margin.right + 18) +
    '" y="24" fill="#17201b" font-size="13" font-weight="800">Arms</text>' +
    report.arms
      .map((arm, index) => {
        const y = 58 + index * 32;
        const color = colors[index % colors.length];
        return (
          '<g><circle cx="' +
          (width - margin.right + 28) +
          '" cy="' +
          (y - 4) +
          '" r="8" fill="' +
          color +
          '"></circle><text x="' +
          (width - margin.right + 44) +
          '" y="' +
          (y - 5) +
          '" fill="#17201b" font-size="11" font-weight="700">' +
          escapeHtml(arm.label) +
          '</text><text x="' +
          (width - margin.right + 44) +
          '" y="' +
          (y + 10) +
          '" fill="#667169" font-size="10" font-family="ui-monospace,SFMono-Regular,Menlo,monospace">' +
          (arm.score * 100).toFixed(2) +
          '% · $' +
          arm.cost.midpoint.toFixed(2) +
          "</text></g>"
        );
      })
      .join("");
  const skipped = report.skipped.length
    ? `<p class="warning"><strong>Excluded incomplete arms:</strong> ${escapeHtml(report.skipped.join("; "))}</p>`
    : "";
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>DeepSWE completed benchmark matrix</title><style>
:root{color-scheme:light;--bg:#f6f7f5;--surface:#fff;--ink:#17201b;--muted:#667169;--line:#d9ded9;--head:#eef2ee;--sig:#e8f3ff;--sig-ink:#165d96;--diag:#f4f5f4;font:15px/1.5 Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink)}main{width:min(1320px,calc(100% - 32px));margin:0 auto;padding:38px 0 54px}h1{margin:0;font-size:clamp(2rem,4vw,3rem);letter-spacing:-.045em}h2{margin:0 0 6px;font-size:1.35rem;letter-spacing:-.02em}.lede,.section-intro{max-width:920px;color:var(--muted)}.lede{margin:8px 0 0}.section{margin-top:34px}.section-intro{margin:0 0 14px}.card{overflow:hidden;background:var(--surface);border:1px solid var(--line);border-radius:16px;box-shadow:0 12px 36px rgba(30,48,38,.07)}.chart{padding:18px 20px 10px}.chart svg{display:block;width:100%;height:auto}.axis{stroke:#98a39b;stroke-width:1.2}.grid{stroke:#e8ece8;stroke-width:1}.tick{fill:var(--muted);font-size:12px}.axis-label{fill:#405047;font-size:12px;font-weight:700}.point{stroke:#fff;stroke-width:3;filter:drop-shadow(0 2px 3px rgba(23,32,27,.2))}.cost-range{stroke-width:2.5}.cost-cap{stroke-width:1.5}.point-label{fill:var(--ink);font-size:13px;font-weight:750}.point-value{fill:var(--muted);font:11px ui-monospace,SFMono-Regular,Menlo,monospace}.chart-note{margin:5px 4px 8px;color:var(--muted);font-size:.78rem}.scroll{overflow-x:auto}table{width:100%;border-collapse:collapse}th,td{padding:15px 14px;border-bottom:1px solid var(--line);vertical-align:middle}thead th{background:var(--head);color:#4d5a51;font-size:.72rem;letter-spacing:.04em;text-transform:uppercase;white-space:nowrap}tbody tr:last-child>*{border-bottom:0}.run{text-align:left;min-width:210px}.run strong,.run small{display:block}.run small{margin-top:2px;color:var(--muted);font-weight:500;text-transform:capitalize}.swatch{float:left;width:10px;height:10px;margin:6px 10px 0 0;border-radius:50%}.reasoning{text-transform:capitalize}.score{font-size:1.05rem;font-weight:750;font-variant-numeric:tabular-nums}.cost,.number{white-space:nowrap;font-variant-numeric:tabular-nums}.number{text-align:right}.range{color:var(--muted)}.summary-table{min-width:780px}.p-table{min-width:860px}.phead{text-align:center;min-width:105px}.pvalue{text-align:center;font:650 .86rem ui-monospace,SFMono-Regular,Menlo,monospace;font-variant-numeric:tabular-nums}.pvalue.significant{background:var(--sig);color:var(--sig-ink);font-weight:800}.pvalue.diagonal{background:var(--diag);color:#9aa19c}.notes{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-top:14px}.note{padding:16px 18px;background:var(--surface);border:1px solid var(--line);border-radius:12px}.note strong{display:block;margin-bottom:3px}.note span{color:var(--muted);font-size:.84rem}.warning{padding:12px 14px;background:#fff7df;border:1px solid #ead28b;border-radius:10px;color:#7a4f00}.hash{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere}footer{margin-top:20px;color:var(--muted);font-size:.76rem}@media(max-width:800px){.notes{grid-template-columns:1fr}main{width:min(100% - 18px,1320px);padding-top:20px}.chart{padding:8px}}
</style></head><body><main><h1>DeepSWE completed benchmark matrix</h1><p class="lede">${report.arms.length} completed arms on the same ${report.selection.tasks.length} selected tasks, with measured cost, calibrated score, and pairwise statistical comparisons.</p>${skipped}<section class="section"><h2>Cost and score</h2><p class="section-intro">Measured arm cost on a logarithmic axis and calibrated estimated full DeepSWE score. Horizontal bars show the provider-accounting cost range.</p><div class="card chart"><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Cost versus calibrated score">${scoreGrid}${costGrid}<line class="axis" x1="${margin.left}" y1="${height - margin.bottom}" x2="${width - margin.right}" y2="${height - margin.bottom}"></line><line class="axis" x1="${margin.left}" y1="${margin.top}" x2="${margin.left}" y2="${height - margin.bottom}"></line><text class="axis-label" x="${margin.left + plotWidth / 2}" y="${height - 18}" text-anchor="middle">Arm cost (USD, logarithmic)</text><text class="axis-label" x="18" y="${margin.top + plotHeight / 2}" text-anchor="middle" transform="rotate(-90 18 ${margin.top + plotHeight / 2})">Estimated full DeepSWE score</text>${chartPoints}</svg><p class="chart-note">Score is shown without error bars. Cost ranges reflect the available provider usage accounting.</p></div></section><section class="section"><h2>Summary</h2><p class="section-intro">One row per completed arm.</p><div class="card scroll"><table class="summary-table"><thead><tr><th>Run</th><th>Reasoning</th><th>Score</th><th>Cost</th><th>Cost range</th><th class="number">Invocations</th></tr></thead><tbody>${summaryRows}</tbody></table></div></section><section class="section"><h2>Pairwise p-values</h2><p class="section-intro">Every off-diagonal cell is the two-sided p-value comparing its row and column arms. The diagonal is intentionally blank.</p><div class="card scroll"><table class="p-table"><thead><tr><th>Run</th>${pHeaders}</tr></thead><tbody>${pRows}</tbody></table></div></section><section class="notes"><div class="note"><strong>Inference</strong><span>Calibrated Welch–Satterthwaite calculation using fixed task weights and calibration slopes.</span></div><div class="note"><strong>Variance</strong><span>Published four-trial per-task F2P variance for each model/reasoning configuration. Agent Layer uses its matching bare configuration’s published variance proxy.</span></div><div class="note"><strong>Reading the grid</strong><span>Blue cells are p&lt;0.05. P-values show evidence of a difference, not direction or practical value.</span></div></section><footer>Matrix <span class="hash">${escapeHtml(report.selectionId)}</span> · generated ${escapeHtml(report.generatedAt)}</footer></main></body></html>`;
}

function main() {
  const options = parseArguments(process.argv.slice(2));
  const selection = readJson(options.selection, "selection");
  validateSelection(selection);
  const snapshotHash = sha256File(options.trials);
  assert(
    snapshotHash === selection.snapshot.sha256,
    `trials snapshot hash ${snapshotHash} does not match selection ${selection.snapshot.sha256}`,
  );
  const snapshot = readJson(options.trials, "published trials snapshot");
  assert(Array.isArray(snapshot.rows), "published trials snapshot has no rows array");
  const selectionID = path.basename(path.resolve(options["matrix-dir"]));
  const computedSelectionID = matrixSelectionID(selection);
  assert(
    computedSelectionID === selectionID,
    `selection identity ${computedSelectionID} does not match matrix ${selectionID}`,
  );
  const discovered = discoverCompletedArms(options["matrix-dir"], selection);
  let arms = discovered.arms;
  const skipped = [...discovered.skipped];
  if (options["additional-baselines-from"]) {
    const additional = discoverCompletedArms(
      options["additional-baselines-from"],
      selection,
    );
    arms = sortArmsForReport([
      ...arms.filter((arm) => arm.mode === "treatment"),
      ...latestBaselinePerConfiguration([...arms, ...additional.arms]),
    ]);
  }
  if (options["additional-arms-from"]) {
    const additional = discoverCompletedArms(
      options["additional-arms-from"],
      selection,
    );
    // The imported matrix is the older one, so an arm present in both keeps the
    // evidence discovered under --matrix-dir.
    const byID = new Map(
      [...additional.arms, ...arms].map((arm) => [arm.id, arm]),
    );
    arms = sortArmsForReport([...byID.values()]);
    skipped.push(...additional.skipped);
  }
  const pValueMatrix = arms.map((left, leftIndex) =>
    arms.map((right, rightIndex) =>
      leftIndex === rightIndex ? null : compareArms(selection, snapshot.rows, left, right),
    ),
  );
  const report = {
    schema: "deepswe-completed-matrix-report-v1",
    generatedAt: new Date().toISOString(),
    selectionId: selectionID,
    snapshotSha256: snapshotHash,
    selection,
    arms,
    skipped,
    pValueMatrix,
    method: {
      name: "calibrated Welch-Satterthwaite",
      tails: "two-sided",
      variance: "published four-trial per-task F2P variance for each arm model/reasoning configuration",
    },
  };
  fs.mkdirSync(path.dirname(options.output), { recursive: true });
  fs.mkdirSync(path.dirname(options["json-output"]), { recursive: true });
  fs.writeFileSync(options["json-output"], `${JSON.stringify(report, null, 2)}\n`);
  fs.writeFileSync(options.output, renderHtml(report));
  process.stdout.write(`Report: ${path.resolve(options.output)}\nJSON: ${path.resolve(options["json-output"])}\n`);
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}

module.exports = { matrixSelectionID };
