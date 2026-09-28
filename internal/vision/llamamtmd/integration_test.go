//go:build integration

package llamamtmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// TestIntegrationRealLlamaMTMD runs the real ffmpeg and llama-mtmd-cli. It
// needs CONTEXT_VIDEO_VLM_MODEL and CONTEXT_VIDEO_VLM_MMPROJ (GGUF model and
// projector paths). CONTEXT_VIDEO_LLAMA_MTMD_CLI and CONTEXT_VIDEO_FFMPEG
// override the binaries found on PATH, and CONTEXT_VIDEO_VLM_VIDEO supplies a
// local video (otherwise a two-second ffmpeg test pattern is generated).
//
//	go test -tags integration -run Integration -v ./internal/vision/llamamtmd
func TestIntegrationRealLlamaMTMD(t *testing.T) {
	model, mmproj := os.Getenv("CONTEXT_VIDEO_VLM_MODEL"), os.Getenv("CONTEXT_VIDEO_VLM_MMPROJ")
	if model == "" || mmproj == "" {
		t.Skip("CONTEXT_VIDEO_VLM_MODEL and CONTEXT_VIDEO_VLM_MMPROJ not set")
	}
	binary := lookup(t, "CONTEXT_VIDEO_LLAMA_MTMD_CLI", "llama-mtmd-cli")
	ffmpeg := lookup(t, "CONTEXT_VIDEO_FFMPEG", "ffmpeg")

	input := os.Getenv("CONTEXT_VIDEO_VLM_VIDEO")
	window := contracts.TimeWindow{StartMs: 0, EndMs: 2000}
	if input == "" {
		input = filepath.Join(t.TempDir(), "testsrc.mp4")
		out, err := exec.Command(ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-t", "2", "-pix_fmt", "yuv420p", "-y", input).CombinedOutput()
		if err != nil {
			t.Fatalf("generate test video: %v: %s", err, out)
		}
	} else {
		window.EndMs = 5000
	}

	a, err := New(Config{Model: model, MMProj: mmproj, Binary: binary, FFmpeg: ffmpeg})
	if err != nil {
		t.Fatal(err)
	}
	segment := contracts.MediaSegment{
		SegmentID: fmt.Sprintf("integration:%d-%d", window.StartMs, window.EndMs),
		Content:   contracts.ContentRef{ContentID: "integration", ContentType: contracts.ContentTypeVOD},
		Window:    window,
	}
	times, err := vision.Uniform{Frames: 1}.FrameTimes(window)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	analysis, err := a.Analyze(ctx, vision.Request{
		Segment:      segment,
		Source:       vision.Source{Kind: vision.SourceLocal, URI: input, Path: input},
		FrameTimesMs: times,
	})
	if err != nil {
		t.Fatal(err)
	}
	obs, err := vision.NewObservation(segment, times, analysis, "integration")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := contracts.NewValidator(os.DirFS(filepath.Join("..", "..", "..", "specs")))
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateJSON(contracts.KindVisualObservation, data); err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Logf("observation: %s", data)
}

func lookup(t *testing.T, env, name string) string {
	t.Helper()
	if v := os.Getenv(env); v != "" {
		return v
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not on PATH and %s not set", name, env)
	}
	return path
}
