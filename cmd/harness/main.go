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
	"strings"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

const validationOnly = "validation-only"

// newPipeline resolves a --pipeline name. M03 only provides the provider-free
// validation-only smoke pipeline.
func newPipeline(name string) (harness.Pipeline, bool) {
	switch name {
	case validationOnly:
		return harness.ValidationOnlyPipeline{}, true
	default:
		return nil, false
	}
}

func pipelineNames() []string {
	return []string{validationOnly}
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
	flags := flag.NewFlagSet("harness", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "experiment YAML file (required)")
	manifestPath := flags.String("manifest", "", "manifest path relative to --dataset-root (required)")
	datasetRoot := flags.String("dataset-root", "dataset", "golden dataset root directory")
	specsRoot := flags.String("specs-root", "specs", "JSON Schema directory")
	outputDir := flags.String("output", "", "result output directory (required)")
	pipelineName := flags.String("pipeline", validationOnly, "pipeline: "+strings.Join(pipelineNames(), ", "))
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
	pipeline, ok := newPipeline(*pipelineName)
	if !ok {
		return usageError{fmt.Sprintf("unknown pipeline %q; available: %s", *pipelineName, strings.Join(pipelineNames(), ", "))}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadFile(*configPath)
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
	runner, err := harness.NewRunner(validator, pipeline)
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
