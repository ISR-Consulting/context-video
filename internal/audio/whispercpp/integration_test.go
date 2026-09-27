//go:build integration

package whispercpp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// TestIntegrationRealWhisper runs the real ffmpeg and whisper-cli. It needs
// CONTEXT_VIDEO_WHISPER_MODEL (ggml model path). CONTEXT_VIDEO_WHISPER_CLI
// and CONTEXT_VIDEO_FFMPEG override the binaries found on PATH,
// CONTEXT_VIDEO_WHISPER_AUDIO supplies a local speech file (otherwise two
// seconds of generated silence are used) and CONTEXT_VIDEO_WHISPER_LANGUAGE
// sets the language (default auto).
//
//	go test -tags integration -run Integration -v ./internal/audio/whispercpp
func TestIntegrationRealWhisper(t *testing.T) {
	model := os.Getenv("CONTEXT_VIDEO_WHISPER_MODEL")
	if model == "" {
		t.Skip("CONTEXT_VIDEO_WHISPER_MODEL not set")
	}
	binary := lookup(t, "CONTEXT_VIDEO_WHISPER_CLI", "whisper-cli")
	ffmpeg := lookup(t, "CONTEXT_VIDEO_FFMPEG", "ffmpeg")

	input := os.Getenv("CONTEXT_VIDEO_WHISPER_AUDIO")
	window := contracts.TimeWindow{StartMs: 0, EndMs: 2000}
	if input == "" {
		input = filepath.Join(t.TempDir(), "silence.wav")
		out, err := exec.Command(ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "2", "-y", input).CombinedOutput()
		if err != nil {
			t.Fatalf("generate silence: %v: %s", err, out)
		}
	} else {
		window.EndMs = 5000
	}

	a, err := New(Config{Model: model, Binary: binary, FFmpeg: ffmpeg, Language: os.Getenv("CONTEXT_VIDEO_WHISPER_LANGUAGE")})
	if err != nil {
		t.Fatal(err)
	}
	segment := contracts.MediaSegment{
		SegmentID: fmt.Sprintf("integration:%d-%d", window.StartMs, window.EndMs),
		Content:   contracts.ContentRef{ContentID: "integration", ContentType: contracts.ContentTypeVOD},
		Window:    window,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	transcription, err := a.Transcribe(ctx, audio.Request{Segment: segment, Source: audio.Source{Kind: audio.SourceLocal, URI: input, Path: input}})
	if err != nil {
		t.Fatal(err)
	}
	if transcription.Confidence != nil {
		t.Fatal("whisper-cli supplies no confidence; none may be reported")
	}
	obs, err := audio.NewObservation(segment, transcription, "integration")
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
	if err := validator.ValidateJSON(contracts.KindAudioObservation, data); err != nil {
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
