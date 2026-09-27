package media_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/config"
	"github.com/ISR-Consulting/context-video/internal/evaluation/harness"
	"github.com/ISR-Consulting/context-video/internal/media"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// TestHarnessBoundary proves a LIVE_SIMULATION pipeline can segment and replay
// each M02 fixture test case inside harness.Pipeline.Process with the M03 APIs
// unchanged.
func TestHarnessBoundary(t *testing.T) {
	cfg, err := config.LoadFile(filepath.Join("..", "..", "configs", "experiments", "multimodal-5s.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IngestionMode != config.IngestionLiveSimulation {
		t.Fatalf("ingestion mode %q", cfg.IngestionMode)
	}
	specsFS := os.DirFS(specsRoot)
	validator := newValidator(t)
	datasetFS := os.DirFS(filepath.Join("..", "evaluation", "dataset", "testdata", "valid"))
	manifest := "manifests/poc-golden-v1.0.json"
	ds, err := harness.LoadDataset(datasetFS, specsFS, manifest)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := media.NewSimulator(media.SystemClock{}, media.Pacing{Instant: true})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	replayed := map[string][]contracts.MediaSegment{}
	pipeline := harness.PipelineFunc(func(ctx context.Context, in harness.PipelineInput) (harness.PipelineOutput, error) {
		tc := in.TestCase.TestCase
		segments, err := media.Segment(media.Source{
			Content: tc.Content, URI: tc.Media.URI, DurationMs: tc.Media.DurationMs,
		}, in.Config.WindowSize)
		if err != nil {
			return harness.PipelineOutput{}, err
		}
		_, err = sim.Replay(ctx, segments, func(e media.Emission) error {
			data, err := json.Marshal(e.Segment)
			if err != nil {
				return err
			}
			if err := validator.ValidateJSON(contracts.KindMediaSegment, data); err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			replayed[tc.TestCaseID] = append(replayed[tc.TestCaseID], e.Segment)
			return nil
		})
		return harness.PipelineOutput{}, err
	})
	runner, err := harness.NewRunner(validator, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), harness.RunInput{Config: cfg, Dataset: ds, ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Metadata.ProcessedTestCases != 2 {
		t.Fatalf("processed %d test cases, want 2", outcome.Metadata.ProcessedTestCases)
	}

	want := map[string]struct {
		content contracts.ContentRef
		uri     string
		windows [][2]int64
	}{
		"football-live": {
			contracts.ContentRef{ContentID: "live-football-001", ContentType: contracts.ContentTypeLive},
			"media/football-live.fixture", [][2]int64{{0, 5000}, {5000, 10000}},
		},
		"visual-vod": {
			contracts.ContentRef{ContentID: "vod-visual-001", ContentType: contracts.ContentTypeVOD},
			"controlled://approved/visual-vod-001", [][2]int64{{0, 5000}, {5000, 8000}},
		},
	}
	if len(replayed) != len(want) {
		t.Fatalf("replayed %d test cases, want %d", len(replayed), len(want))
	}
	for id, w := range want {
		got := replayed[id]
		if len(got) != len(w.windows) {
			t.Fatalf("%s: %d segments, want %d", id, len(got), len(w.windows))
		}
		for i, segment := range got {
			if segment.Window.StartMs != w.windows[i][0] || segment.Window.EndMs != w.windows[i][1] {
				t.Fatalf("%s segment %d window %+v, want %v", id, i, segment.Window, w.windows[i])
			}
			if segment.Content != w.content || segment.SourceURI == nil || *segment.SourceURI != w.uri {
				t.Fatalf("%s segment %d = %+v", id, i, segment)
			}
		}
	}
}
