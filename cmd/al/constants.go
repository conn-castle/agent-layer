package main

const (
	commandInit    = "init"
	commandUpgrade = "upgrade"
	unknownVersion = "unknown"
	noSyncFlag     = "--no-sync"

	issueUnrecognizedConfigKeys          = "unrecognized_config_keys"
	issueUnresolvedConfigPlaceholders    = "unresolved_config_placeholders"
	issueProcessEnvOverridesDotenv       = "process_env_overrides_dotenv"
	issueIgnoredEmptyDotenvAssignments   = "ignored_empty_dotenv_assignments"
	issuePathExpansionAnomalies          = "path_expansion_anomalies"
	issueVSCodeNoSyncOutputsStale        = "vscode_no_sync_outputs_stale"
	issueFloatingExternalDependencySpecs = "floating_external_dependency_specs"
	issueStaleDisabledAgentArtifacts     = "stale_disabled_agent_artifacts"
	issueMissingRequiredConfigFields     = "missing_required_config_fields"
)
