// Command harness runs the M03 evaluation harness: it loads an experiment
// configuration and a golden dataset, executes a pipeline over every test case
// and persists the canonical ExperimentResult.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/audio/providers"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/pipeline"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

const (
	validationOnly = "validation-only"
	audioPipeline  = "audio"
)

func pipelineNames() []string {
	return []string{audioPipeline, validationOnly}
}

// newPipeline builds the pipeline selected by --pipeline. The audio pipeline
// resolves cfg.Audio.Provider through registry and never names a vendor.
func newPipeline(name string, cfg config.Config, registry *audio.Registry, audioOpts audio.Options, datasetRoot string) (harness.Pipeline, error) {
	switch name {
	case validationOnly:
		return harness.ValidationOnlyPipeline{}, nil
	case audioPipeline:
		if !cfg.Audio.Enabled {
			return nil, fmt.Errorf("pipeline %q requires audio.enabled: true in experiment %s", name, cfg.ExperimentID)
		}
		transcriber, err := registry.New(cfg.Audio.Provider, audioOpts)
		if err != nil {
			return nil, err
		}
		audioOnly, err := pipeline.NewAudio(cfg.Audio.Provider, transcriber, pipeline.DirResolver(datasetRoot))
		if err != nil {
			return nil, err
		}
		return audioOnly, nil
	default:
		return nil, usageError{fmt.Sprintf("unknown pipeline %q; available: %s", name, strings.Join(pipelineNames(), ", "))}
	}
}

// optionFlags collects repeatable key=value flags.
type optionFlags audio.Options

func (o optionFlags) String() string { return "" }

func (o optionFlags) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("want key=value, got %q", value)
	}
	if _, dup := o[key]; dup {
		return fmt.Errorf("duplicate option %q", key)
	}
	o[key] = val
	return nil
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "harness: "+err.Error())
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	return runWith(args, stdout, stderr, providers.Default())
}

func runWith(args []string, stdout, stderr io.Writer, registry *audio.Registry) error {
	flags := flag.NewFlagSet("harness", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "experiment YAML file (required)")
	manifestPath := flags.String("manifest", "", "manifest path relative to --dataset-root (required)")
	datasetRoot := flags.String("dataset-root", "dataset", "golden dataset root directory")
	specsRoot := flags.String("specs-root", "specs", "JSON Schema directory")
	outputDir := flags.String("output", "", "result output directory (required)")
	pipelineName := flags.String("pipeline", validationOnly, "pipeline: "+strings.Join(pipelineNames(), ", "))
	audioOpts := audio.Options{}
	flags.Var(optionFlags(audioOpts), "audio-option",
		"audio provider option key=value, repeatable (audio pipeline only; keys depend on audio.provider)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			fmt.Fprintln(stderr, "usage: harness --config FILE --manifest PATH --output DIR [flags]")
			flags.PrintDefaults()
			return nil
		}
		return usageError{err.Error()}
	}
	if flags.NArg() > 0 {
		return usageError{fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " "))}
	}
	var missing []string
	for _, required := range []struct{ name, value string }{
		{"--config", *configPath}, {"--manifest", *manifestPath}, {"--output", *outputDir},
	} {
		if required.value == "" {
			missing = append(missing, required.name)
		}
	}
	if len(missing) > 0 {
		return usageError{"missing required flags: " + strings.Join(missing, ", ")}
	}
	if !slices.Contains(pipelineNames(), *pipelineName) {
		return usageError{fmt.Sprintf("unknown pipeline %q; available: %s", *pipelineName, strings.Join(pipelineNames(), ", "))}
	}
	if len(audioOpts) > 0 && *pipelineName != audioPipeline {
		return usageError{fmt.Sprintf("--audio-option is only valid with --pipeline %s", audioPipeline)}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		return err
	}
	selected, err := newPipeline(*pipelineName, cfg, registry, audioOpts, *datasetRoot)
	if err != nil {
		return err
	}
	specsFS := os.DirFS(*specsRoot)
	validator, err := contracts.NewValidator(specsFS)
	if err != nil {
		return &harness.Error{Stage: harness.StageSpecs, Path: *specsRoot, Err: err}
	}
	ds, err := harness.LoadDataset(os.DirFS(*datasetRoot), specsFS, *manifestPath)
	if err != nil {
		return err
	}
	runner, err := harness.NewRunner(validator, selected)
	if err != nil {
		return err
	}
	outcome, err := runner.Run(ctx, harness.RunInput{Config: cfg, Dataset: ds, ManifestPath: *manifestPath})
	if err != nil {
		return err
	}
	persister, err := harness.NewPersister(validator, *outputDir)
	if err != nil {
		return err
	}
	path, err := persister.Write(outcome.Result, outcome.Dataset)
	if err != nil {
		return err
	}

	meta := outcome.Metadata
	fmt.Fprintf(stdout, "result: %s\n", path)
	fmt.Fprintf(stdout, "experiment: %s\n", outcome.Result.ExperimentID)
	fmt.Fprintf(stdout, "pipeline: %s\n", *pipelineName)
	fmt.Fprintf(stdout, "dataset: %s v%s\n", outcome.Dataset.DatasetID, outcome.Dataset.DatasetVersion)
	fmt.Fprintf(stdout, "test cases processed: %d\n", meta.ProcessedTestCases)
	fmt.Fprintf(stdout, "elapsed: %s\n", meta.Elapsed)
	fmt.Fprintf(stdout, "outputs: audio observations=%d visual observations=%d context events=%d\n",
		meta.AudioObservations, meta.VisualObservations, meta.ContextEvents)
	return nil
}
