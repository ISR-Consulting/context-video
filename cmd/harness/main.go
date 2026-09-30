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
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	contextproviders "github.com/ISR-Consulting/context-video/internal/context/providers"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/pipeline"
	"github.com/ISR-Consulting/context-video/internal/vision"
	visionproviders "github.com/ISR-Consulting/context-video/internal/vision/providers"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

const (
	validationOnly = "validation-only"
	audioPipeline  = "audio"
	visionPipeline = "vision"
	multimodal     = "multimodal"
)

func pipelineNames() []string {
	return []string{audioPipeline, validationOnly, visionPipeline, multimodal}
}

// registries holds the provider registries the harness selects adapters from.
type registries struct {
	audio   *audio.Registry
	vision  *vision.Registry
	context *contextcore.Registry
}

// pipelineOptions are the opaque adapter options collected from the CLI.
type pipelineOptions struct {
	audio   audio.Options
	vision  vision.Options
	context contextcore.Options
}

// newPipeline builds the pipeline selected by --pipeline. The audio, vision
// and multimodal pipelines resolve cfg.Audio.Provider, cfg.Vision.Provider and
// cfg.FusionProvider through the registries and never name a vendor.
func newPipeline(name string, cfg config.Config, regs registries, opts pipelineOptions, datasetRoot string) (harness.Pipeline, error) {
	switch name {
	case validationOnly:
		return harness.ValidationOnlyPipeline{}, nil
	case audioPipeline:
		if !cfg.Audio.Enabled {
			return nil, fmt.Errorf("pipeline %q requires audio.enabled: true in experiment %s", name, cfg.ExperimentID)
		}
		transcriber, err := regs.audio.New(cfg.Audio.Provider, opts.audio)
		if err != nil {
			return nil, err
		}
		audioOnly, err := pipeline.NewAudio(cfg.Audio.Provider, transcriber, pipeline.DirResolver(datasetRoot))
		if err != nil {
			return nil, err
		}
		return audioOnly, nil
	case visionPipeline:
		if !cfg.Vision.Enabled {
			return nil, fmt.Errorf("pipeline %q requires vision.enabled: true in experiment %s", name, cfg.ExperimentID)
		}
		if _, err := vision.ParseSampling(cfg.Vision.Sampling); err != nil {
			return nil, fmt.Errorf("experiment %s vision.sampling: %w", cfg.ExperimentID, err)
		}
		analyzer, err := regs.vision.New(cfg.Vision.Provider, opts.vision)
		if err != nil {
			return nil, err
		}
		visionOnly, err := pipeline.NewVision(cfg.Vision.Provider, analyzer, pipeline.VisionDirResolver(datasetRoot))
		if err != nil {
			return nil, err
		}
		return visionOnly, nil
	case multimodal:
		return newMultimodal(cfg, regs, opts, datasetRoot)
	default:
		return nil, usageError{fmt.Sprintf("unknown pipeline %q; available: %s", name, strings.Join(pipelineNames(), ", "))}
	}
}

// newMultimodal builds the multimodal pipeline for the modalities cfg
// enables, with the reasoner named by fusion.provider.
func newMultimodal(cfg config.Config, regs registries, opts pipelineOptions, datasetRoot string) (harness.Pipeline, error) {
	if len(opts.audio) > 0 && !cfg.Audio.Enabled {
		return nil, fmt.Errorf("--audio-option given but experiment %s disables audio", cfg.ExperimentID)
	}
	if len(opts.vision) > 0 && !cfg.Vision.Enabled {
		return nil, fmt.Errorf("--vision-option given but experiment %s disables vision", cfg.ExperimentID)
	}
	var mm pipeline.MultimodalConfig
	if cfg.Vision.Enabled {
		if _, err := vision.ParseSampling(cfg.Vision.Sampling); err != nil {
			return nil, fmt.Errorf("experiment %s vision.sampling: %w", cfg.ExperimentID, err)
		}
	}
	reasoner, err := regs.context.New(cfg.FusionProvider, opts.context)
	if err != nil {
		return nil, err
	}
	engine, err := contextcore.NewEngine(cfg.FusionProvider, reasoner)
	if err != nil {
		return nil, err
	}
	mm.Engine = engine
	if cfg.Audio.Enabled {
		transcriber, err := regs.audio.New(cfg.Audio.Provider, opts.audio)
		if err != nil {
			return nil, err
		}
		mm.AudioProvider, mm.Transcriber, mm.AudioResolve = cfg.Audio.Provider, transcriber, pipeline.DirResolver(datasetRoot)
	}
	if cfg.Vision.Enabled {
		analyzer, err := regs.vision.New(cfg.Vision.Provider, opts.vision)
		if err != nil {
			return nil, err
		}
		mm.VisionProvider, mm.Analyzer, mm.VisionResolve = cfg.Vision.Provider, analyzer, pipeline.VisionDirResolver(datasetRoot)
	}
	return pipeline.NewMultimodal(mm)
}

// optionFlags collects repeatable key=value flags.
type optionFlags map[string]string

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
	return runWith(args, stdout, stderr, registries{
		audio:   providers.Default(),
		vision:  visionproviders.Default(),
		context: contextproviders.Default(),
	})
}

func runWith(args []string, stdout, stderr io.Writer, regs registries) error {
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
		"audio provider option key=value, repeatable (audio and multimodal pipelines; keys depend on audio.provider)")
	visionOpts := vision.Options{}
	flags.Var(optionFlags(visionOpts), "vision-option",
		"vision provider option key=value, repeatable (vision and multimodal pipelines; keys depend on vision.provider)")
	contextOpts := contextcore.Options{}
	flags.Var(optionFlags(contextOpts), "context-option",
		"context reasoning provider option key=value, repeatable (multimodal pipeline only; keys depend on fusion.provider)")
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
	if len(audioOpts) > 0 && *pipelineName != audioPipeline && *pipelineName != multimodal {
		return usageError{fmt.Sprintf("--audio-option is only valid with --pipeline %s or %s", audioPipeline, multimodal)}
	}
	if len(visionOpts) > 0 && *pipelineName != visionPipeline && *pipelineName != multimodal {
		return usageError{fmt.Sprintf("--vision-option is only valid with --pipeline %s or %s", visionPipeline, multimodal)}
	}
	if len(contextOpts) > 0 && *pipelineName != multimodal {
		return usageError{fmt.Sprintf("--context-option is only valid with --pipeline %s", multimodal)}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		return err
	}
	selected, err := newPipeline(*pipelineName, cfg, regs, pipelineOptions{audio: audioOpts, vision: visionOpts, context: contextOpts}, *datasetRoot)
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
