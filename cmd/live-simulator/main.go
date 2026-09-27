// Command live-simulator replays golden dataset media as deterministic
// MediaSegment windows in media time and writes them to stdout as JSON Lines.
// It never opens or decodes media and calls no provider.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "live-simulator: "+err.Error())
	}
	os.Exit(exitCode(err))
}

// exitCode maps a run error to 0 (success or help), 2 (usage) or 1 (runtime,
// including cancellation).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var usage usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

// replayCase is one test case whose segments are ready to be replayed. lines
// holds each segment's validated JSON encoding, in segment order.
type replayCase struct {
	testCaseID string
	content    contracts.ContentRef
	segments   []contracts.MediaSegment
	lines      [][]byte
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("live-simulator", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "experiment YAML file (required)")
	manifestPath := flags.String("manifest", "", "manifest path relative to --dataset-root (required)")
	datasetRoot := flags.String("dataset-root", "dataset", "golden dataset root directory")
	specsRoot := flags.String("specs-root", "specs", "JSON Schema directory")
	testCaseID := flags.String("test-case", "", "replay only this test case (default: all, in manifest order)")
	speed := flags.Float64("speed", 1.0, "media-time speed factor (1 = real time)")
	instant := flags.Bool("instant", false, "emit every segment without waiting")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			fmt.Fprintln(stderr, "usage: live-simulator --config FILE --manifest PATH [--speed X | --instant] [flags]")
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
		{"--config", *configPath}, {"--manifest", *manifestPath},
	} {
		if required.value == "" {
			missing = append(missing, required.name)
		}
	}
	if len(missing) > 0 {
		return usageError{"missing required flags: " + strings.Join(missing, ", ")}
	}
	speedSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "speed" {
			speedSet = true
		}
	})
	if speedSet && *instant {
		return usageError{"--speed and --instant are mutually exclusive"}
	}
	pacing := media.Pacing{Instant: *instant, Speed: *speed}
	if err := pacing.Validate(); err != nil {
		return usageError{"--speed: " + err.Error()}
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
		return fmt.Errorf("load specs %s: %w", *specsRoot, err)
	}
	loader, err := dataset.NewLoader(os.DirFS(*datasetRoot), specsFS)
	if err != nil {
		return fmt.Errorf("load dataset schemas: %w", err)
	}
	ds, err := loader.LoadDataset(*manifestPath)
	if err != nil {
		return fmt.Errorf("load dataset %s: %w", *manifestPath, err)
	}

	cases, err := prepare(ds, *testCaseID, cfg.WindowSize, validator)
	if err != nil {
		return err
	}
	simulator, err := media.NewSimulator(media.SystemClock{}, pacing)
	if err != nil {
		return err
	}
	for _, c := range cases {
		started := time.Now()
		emitted, err := simulator.Replay(ctx, c.segments, func(e media.Emission) error {
			_, err := stdout.Write(c.lines[e.Sequence])
			return err
		})
		if err != nil {
			return fmt.Errorf("test case %s: %w", c.testCaseID, err)
		}
		fmt.Fprintf(stderr, "test case %s: content %s (%s): %d segments, window %s, pacing %s, elapsed %s\n",
			c.testCaseID, c.content.ContentID, c.content.ContentType, emitted, cfg.WindowSizeRaw,
			describePacing(pacing), time.Since(started))
	}
	return nil
}

// prepare segments every selected test case and schema- and domain-validates
// each segment before anything is written.
func prepare(ds dataset.Dataset, testCaseID string, window time.Duration, validator *contracts.Validator) ([]replayCase, error) {
	var cases []replayCase
	for _, loaded := range ds.TestCases {
		tc := loaded.TestCase
		if testCaseID != "" && tc.TestCaseID != testCaseID {
			continue
		}
		segments, err := media.Segment(media.Source{
			Content: tc.Content, URI: tc.Media.URI, DurationMs: tc.Media.DurationMs,
		}, window)
		if err != nil {
			return nil, fmt.Errorf("test case %s: %w", tc.TestCaseID, err)
		}
		lines := make([][]byte, len(segments))
		for i, segment := range segments {
			data, err := json.Marshal(segment)
			if err != nil {
				return nil, fmt.Errorf("test case %s: segment %s: %w", tc.TestCaseID, segment.SegmentID, err)
			}
			if err := validator.ValidateJSON(contracts.KindMediaSegment, data); err != nil {
				return nil, fmt.Errorf("test case %s: segment %s: %w", tc.TestCaseID, segment.SegmentID, err)
			}
			if err := contracts.Validate(segment); err != nil {
				return nil, fmt.Errorf("test case %s: segment %s: %w", tc.TestCaseID, segment.SegmentID, err)
			}
			lines[i] = append(data, '\n')
		}
		cases = append(cases, replayCase{testCaseID: tc.TestCaseID, content: tc.Content, segments: segments, lines: lines})
	}
	if testCaseID != "" && len(cases) == 0 {
		ids := make([]string, 0, len(ds.TestCases))
		for _, loaded := range ds.TestCases {
			ids = append(ids, loaded.TestCase.TestCaseID)
		}
		return nil, usageError{fmt.Sprintf("unknown test case %q; available: %s", testCaseID, strings.Join(ids, ", "))}
	}
	return cases, nil
}

func describePacing(p media.Pacing) string {
	if p.Instant {
		return "instant"
	}
	return fmt.Sprintf("speed %gx", p.Speed)
}
