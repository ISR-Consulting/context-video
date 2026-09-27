package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDomainValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing experiment section", raw: remove(validYAML, "experiment:\n  id: E04\n  name: multimodal-5s\n"), want: "experiment: section is required"},
		{name: "missing window section", raw: remove(validYAML, "window:\n  size: 5s\n"), want: "window: section is required"},
		{name: "missing schema section", raw: remove(validYAML, "schema:\n  context_event: v1\n"), want: "schema: section is required"},
		{name: "missing experiment id", raw: remove(validYAML, "  id: E04\n"), want: "experiment.id: is required"},
		{name: "blank experiment name", raw: strings.Replace(validYAML, "name: multimodal-5s", "name: \"  \"", 1), want: "experiment.name: must not be blank"},
		{name: "unsupported ingestion", raw: strings.Replace(validYAML, "LIVE_SIMULATION", "BATCH", 1), want: `ingestion.mode: unsupported value "BATCH"`},
		{name: "missing window size", raw: strings.Replace(validYAML, "window:\n  size: 5s\n", "window: {}\n", 1), want: "window.size: is required"},
		{name: "unparseable window", raw: strings.Replace(validYAML, "size: 5s", "size: five", 1), want: "window.size: invalid duration"},
		{name: "unitless window", raw: strings.Replace(validYAML, "size: 5s", "size: 5", 1), want: "window.size: invalid duration"},
		{name: "zero window", raw: strings.Replace(validYAML, "size: 5s", "size: 0s", 1), want: "window.size: must be positive"},
		{name: "negative window", raw: strings.Replace(validYAML, "size: 5s", "size: -5s", 1), want: "window.size: must be positive"},
		{name: "sub-millisecond window", raw: strings.Replace(validYAML, "size: 5s", "size: 500us", 1), want: "window.size: must be a whole number of milliseconds"},
		{name: "fractional millisecond window", raw: strings.Replace(validYAML, "size: 5s", "size: 1500us", 1), want: "window.size: must be a whole number of milliseconds"},
		{name: "missing audio enabled", raw: strings.Replace(validYAML, "audio:\n  enabled: true\n", "audio:\n", 1), want: "audio.enabled: is required"},
		{name: "no modalities", raw: disableAudio(disableVision(validYAML)), want: "at least one of audio or vision must be enabled"},
		{name: "enabled audio without provider", raw: strings.Replace(validYAML, "audio:\n  enabled: true\n  provider: TBD\n", "audio:\n  enabled: true\n", 1), want: "audio.provider: is required"},
		{name: "disabled audio with provider", raw: strings.Replace(validYAML, "audio:\n  enabled: true", "audio:\n  enabled: false", 1), want: "audio.provider: must be omitted when audio is disabled"},
		{name: "enabled vision without sampling", raw: remove(validYAML, "  sampling: TBD\n"), want: "vision.sampling: is required"},
		{name: "enabled vision with blank provider", raw: strings.Replace(validYAML, "vision:\n  enabled: true\n  provider: TBD", "vision:\n  enabled: true\n  provider: \"\"", 1), want: "vision.provider: must not be blank"},
		{name: "disabled vision with options", raw: strings.Replace(validYAML, "vision:\n  enabled: true", "vision:\n  enabled: false", 1), want: "vision.provider: must be omitted when vision is disabled; vision.sampling: must be omitted when vision is disabled"},
		{name: "missing fusion provider", raw: strings.Replace(validYAML, "fusion:\n  provider: TBD\n", "fusion: {}\n", 1), want: "fusion.provider: is required"},
		{name: "missing schema selector", raw: strings.Replace(validYAML, "  context_event: v1\n", "  context_event: \"\"\n", 1), want: "schema.context_event: must not be blank"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("test.yaml", []byte(tc.raw))
			assertStage(t, err, StageDomain, tc.want)
		})
	}
}

func TestDomainValidationReportsAllIssuesInOrder(t *testing.T) {
	raw := "experiment: {}\ningestion: {}\nwindow: {}\naudio: {}\nvision: {}\nfusion: {}\nschema: {}\n"
	_, err := Parse("test.yaml", []byte(raw))
	assertStage(t, err, StageDomain, "")
	want := "config domain: test.yaml: experiment.id: is required; experiment.name: is required; " +
		"ingestion.mode: is required; window.size: is required; audio.enabled: is required; " +
		"vision.enabled: is required; fusion.provider: is required; schema.context_event: is required"
	if err.Error() != want {
		t.Fatalf("error:\n got %s\nwant %s", err.Error(), want)
	}
}

func TestUnsupportedSchemaSelector(t *testing.T) {
	for _, selector := range []string{"v2", "1.0", "V1"} {
		t.Run(selector, func(t *testing.T) {
			raw := strings.Replace(validYAML, "context_event: v1", "context_event: \""+selector+"\"", 1)
			_, err := Parse("test.yaml", []byte(raw))
			assertStage(t, err, StageSchemaSelector, "unsupported selector")
		})
	}
}

func TestSingleModalityConfigsAreValid(t *testing.T) {
	if _, err := Parse("audio.yaml", []byte(disableVision(validYAML))); err != nil {
		t.Fatalf("audio only: %v", err)
	}
	if _, err := Parse("vision.yaml", []byte(disableAudio(validYAML))); err != nil {
		t.Fatalf("vision only: %v", err)
	}
}

func TestSnapshotPreservesYAMLSectionsAndDataset(t *testing.T) {
	cfg, err := Parse("test.yaml", []byte(disableAudio(validYAML)))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := cfg.Snapshot(DatasetIdentity{DatasetID: "poc-golden", DatasetVersion: "1.0", ManifestPath: "manifests/poc-golden-v1.0.json"})
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"audio":{"enabled":false},` +
		`"dataset":{"datasetId":"poc-golden","datasetVersion":"1.0","manifestPath":"manifests/poc-golden-v1.0.json"},` +
		`"experiment":{"id":"E04","name":"multimodal-5s"},` +
		`"fusion":{"provider":"TBD"},` +
		`"ingestion":{"mode":"LIVE_SIMULATION"},` +
		`"schema":{"context_event":"v1"},` +
		`"vision":{"enabled":true,"provider":"TBD","sampling":"TBD"},` +
		`"window":{"size":"5s"}}`
	if string(data) != want {
		t.Fatalf("snapshot:\n got %s\nwant %s", data, want)
	}
}

func remove(s, part string) string {
	if !strings.Contains(s, part) {
		panic("fixture does not contain " + part)
	}
	return strings.Replace(s, part, "", 1)
}

func disableAudio(s string) string {
	return strings.Replace(s, "audio:\n  enabled: true\n  provider: TBD\n", "audio:\n  enabled: false\n", 1)
}

func disableVision(s string) string {
	return strings.Replace(s, "vision:\n  enabled: true\n  provider: TBD\n  sampling: TBD\n", "vision:\n  enabled: false\n", 1)
}
