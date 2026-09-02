package benchmark

import (
	"path/filepath"
	"strings"
)

func matrixSelectionFixture() matrixSelection {
	var selection matrixSelection
	selection.Schema, selection.SchemaVersion = matrixSelectionSchema, matrixSelectionSchemaVersionV2
	selection.Snapshot.URL, selection.Snapshot.SHA256 = DeepSWETrialsSourceURL, strings.Repeat("a", 64)
	selection.Selector.Model, selection.Selector.Reasoning, selection.Selector.BudgetUSD, selection.Selector.IterationsPerTask = publishedLuna, effortLow, .2, 1
	selection.EstimatedPublishedSpendUSD = .2
	selection.Tasks = []matrixSelectionTask{{ID: "first-task", Repetitions: 1, Weight: .25, Calibration: struct {
		Intercept float64 `json:"intercept"`
		Slope     float64 `json:"slope"`
	}{.1, .8}, PublishedMeanCostUSD: .1}, {ID: "second-task", Repetitions: 1, Weight: .75, Calibration: struct {
		Intercept float64 `json:"intercept"`
		Slope     float64 `json:"slope"`
	}{.2, .5}, PublishedMeanCostUSD: .1}}
	return selection
}

func matrixSelectionFixtureV3(sampleVariance float64) matrixSelection {
	selection := matrixSelectionFixture()
	selection.SchemaVersion = matrixSelectionSchemaVersion
	for index := range selection.Tasks {
		selection.Tasks[index].PublishedVariance = lunaPublishedVariance(sampleVariance)
	}
	return selection
}

func lunaPublishedVariance(sampleVariance float64) *matrixPublishedTaskVariance {
	return &matrixPublishedTaskVariance{
		ConfigurationID:     publishedLuna + "::" + effortLow,
		PublishedModel:      publishedLuna,
		PublishedReasoning:  effortLow,
		SampleSize:          4,
		SampleVariance:      sampleVariance,
		VarianceEstimator:   publishedVarianceEstimator,
		VarianceDenominator: publishedVarianceDenominator,
	}
}

func matrixArmFixture(root, label, mode string, model Model, effort string, tasks []benchmarkPlanTask) matrixArm {
	loaded := loadedBenchmarkPlan{Model: model, Effort: effort}
	loaded.Plan.Tasks = append([]benchmarkPlanTask(nil), tasks...)
	return matrixArm{ID: strings.Repeat("c", 64), Label: label, Mode: mode, StateDir: filepath.Join(root, strings.ReplaceAll(label, " ", "-")), Loaded: loaded, IgnoreProviderClientInManifest: true}
}
