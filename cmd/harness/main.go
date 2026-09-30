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
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/audio/providers"
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	contextproviders "github.com/ISR-Consulting/context-video/internal/context/providers"
	"github.com/ISR-Consulting/context-video/internal/evaluation/artifacts"
	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/internal/pipeline"
	"github.com/ISR-Consulting/context-video/internal/telemetry"
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
	// recorder, set for the multimodal pipeline, paces segments and times
	// every port call; recordErrors keeps the run going past failed segments.
	recorder     *telemetry.Recorder
	recordErrors bool
}

const (
	pacingInstant = telemetry.PacingInstant
	pacingLive    = telemetry.PacingLive
	errorsFail    = "fail"
	errorsRecord  = "record"
)

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
	rec := opts.recorder
	if rec != nil {
		reasoner = rec.Reasoner(reasoner)
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
		if rec != nil {
			transcriber = rec.Transcriber(transcriber)
		}
		mm.AudioProvider, mm.Transcriber, mm.AudioResolve = cfg.Audio.Provider, transcriber, pipeline.DirResolver(datasetRoot)
	}
	if cfg.Vision.Enabled {
		analyzer, err := regs.vision.New(cfg.Vision.Provider, opts.vision)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			analyzer = rec.Analyzer(analyzer)
		}
		mm.VisionProvider, mm.Analyzer, mm.VisionResolve = cfg.Vision.Provider, analyzer, pipeline.VisionDirResolver(datasetRoot)
	}
	var mmOpts []pipeline.MultimodalOption
	if rec != nil {
		mmOpts = append(mmOpts, pipeline.WithSegmentObserver(rec))
	}
	if opts.recordErrors {
		mmOpts = append(mmOpts, pipeline.WithRecordedSegmentErrors())
	}
	return pipeline.NewMultimodal(mm, mmOpts...)
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
	pacingName := flags.String("pacing", pacingInstant,
		"multimodal only: instant (segments back to back, VOD-like) or live (M04 replay in media time)")
	speed := flags.Float64("speed", 1, "multimodal --pacing live only: media-time speed factor (1 = real time)")
	segmentErrors := flags.String("on-segment-error", errorsFail,
		"multimodal only: fail (abort the run) or record (trace the failed segment and continue)")
	costPerHour := flags.Float64("cost-per-hour", -1,
		"multimodal only: host price in USD per wall-clock hour; enables costPerVideoHour")
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
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, name := range []string{"pacing", "speed", "on-segment-error", "cost-per-hour"} {
		if set[name] && *pipelineName != multimodal {
			return usageError{fmt.Sprintf("--%s is only valid with --pipeline %s", name, multimodal)}
		}
	}
	pacing := media.Pacing{Instant: true}
	switch *pacingName {
	case pacingInstant:
		if set["speed"] {
			return usageError{"--speed is only valid with --pacing live"}
		}
	case pacingLive:
		pacing = media.Pacing{Speed: *speed}
		if err := pacing.Validate(); err != nil {
			return usageError{"--speed: " + err.Error()}
		}
	default:
		return usageError{fmt.Sprintf("unknown --pacing %q; available: %s, %s", *pacingName, pacingInstant, pacingLive)}
	}
	if *segmentErrors != errorsFail && *segmentErrors != errorsRecord {
		return usageError{fmt.Sprintf("unknown --on-segment-error %q; available: %s, %s", *segmentErrors, errorsFail, errorsRecord)}
	}
	var price *float64
	if set["cost-per-hour"] {
		if math.IsNaN(*costPerHour) || math.IsInf(*costPerHour, 0) || *costPerHour < 0 {
			return usageError{fmt.Sprintf("--cost-per-hour must be a finite number >= 0, got %v", *costPerHour)}
		}
		price = costPerHour
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		return err
	}
	popts := pipelineOptions{audio: audioOpts, vision: visionOpts, context: contextOpts, recordErrors: *segmentErrors == errorsRecord}
	if *pipelineName == multimodal {
		rec, err := telemetry.NewRecorder(telemetry.RecorderConfig{ExperimentID: cfg.ExperimentID, Pacing: pacing, Progress: stderr})
		if err != nil {
			return err
		}
		popts.recorder = rec
	}
	selected, err := newPipeline(*pipelineName, cfg, regs, popts, *datasetRoot)
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
	identity := config.DatasetIdentity{
		DatasetID: ds.Manifest.DatasetID, DatasetVersion: ds.Manifest.DatasetVersion, ManifestPath: *manifestPath,
	}
	var run *m08Run
	if popts.recorder != nil {
		dir, err := artifacts.Create(*outputDir, cfg.ExperimentID)
		if err != nil {
			return err
		}
		defer dir.Close()
		popts.recorder.SetSink(dir.WriteTrace)
		run = &m08Run{dir: dir, recorder: popts.recorder, manifest: artifacts.RunManifest{
			FormatVersion: artifacts.ManifestFormatVersion,
			ExperimentID:  cfg.ExperimentID,
			Pipeline:      *pipelineName,
			Config:        filepath.Base(*configPath),
			Dataset:       artifacts.Dataset{DatasetID: identity.DatasetID, DatasetVersion: identity.DatasetVersion, ManifestPath: identity.ManifestPath},
			Pacing:        *pacingName,
			SegmentErrors: *segmentErrors,
			CostPerHour:   price,
			StartedAt:     time.Now().UTC(),
			Host:          artifacts.CurrentHost(),
		}}
		if popts.recorder.Live() {
			run.manifest.Speed = speed
		}
		if run.manifest.Options, err = describeOptions(audioOpts, visionOpts, contextOpts); err != nil {
			return err
		}
	}
	outcome, err := runner.Run(ctx, harness.RunInput{Config: cfg, Dataset: ds, ManifestPath: *manifestPath})
	if err == nil && run != nil {
		err = run.recorder.Err()
	}
	if err != nil {
		if run != nil {
			if mErr := run.finish(err); mErr != nil {
				return errors.Join(err, mErr)
			}
		}
		return err
	}
	var summary telemetry.Summary
	if run != nil {
		in := telemetry.RunInput{
			ExperimentID: cfg.ExperimentID, Live: run.recorder.Live(), Records: run.recorder.Records(),
			Cases: outcome.Cases, CostPerHour: price,
		}
		outcome.Result.Metrics = telemetry.Metrics(in)
		summary = telemetry.Summarize(in)
	}
	persister, err := harness.NewPersister(validator, *outputDir)
	if err != nil {
		return err
	}
	path, err := persister.Write(outcome.Result, outcome.Dataset)
	if err != nil {
		return err
	}
	if run != nil {
		if err := run.dir.WriteRaw(validator, outcome.Cases); err != nil {
			return err
		}
		if err := run.dir.WriteJSON("summary.json", summary); err != nil {
			return err
		}
		if err := run.finish(nil); err != nil {
			return err
		}
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
	if run != nil {
		fmt.Fprintf(stdout, "artifacts: %s\n", run.dir.Path())
		fmt.Fprintf(stdout, "pacing: %s\n", describePacing(pacing))
		fmt.Fprintf(stdout, "segments: %d (failed %d)\n", summary.Segments, summary.FailedSegments)
		if summary.RealTimeFactor != nil {
			fmt.Fprintf(stdout, "real-time factor: %.2f\n", *summary.RealTimeFactor)
		}
	}
	return nil
}

// m08Run holds the per-run artifact state of a multimodal run.
type m08Run struct {
	dir      *artifacts.Dir
	recorder *telemetry.Recorder
	manifest artifacts.RunManifest
}

// finish writes run-manifest.json with the run's final status. It is written
// for failed runs too, next to the partial trace.
func (r *m08Run) finish(runErr error) error {
	r.manifest.FinishedAt = time.Now().UTC()
	r.manifest.Status = "completed"
	if runErr != nil {
		r.manifest.Status = "failed"
		r.manifest.Error = runErr.Error()
	}
	return r.dir.WriteJSON("run-manifest.json", r.manifest)
}

func describeOptions(a audio.Options, v vision.Options, c contextcore.Options) (artifacts.Options, error) {
	var out artifacts.Options
	var err error
	if out.Audio, err = artifacts.DescribeOptions(a); err != nil {
		return out, err
	}
	if out.Vision, err = artifacts.DescribeOptions(v); err != nil {
		return out, err
	}
	out.Context, err = artifacts.DescribeOptions(c)
	return out, err
}

func describePacing(p media.Pacing) string {
	if p.Instant {
		return pacingInstant
	}
	return fmt.Sprintf("%s %gx", pacingLive, p.Speed)
}
