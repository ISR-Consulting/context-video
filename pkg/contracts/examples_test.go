package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestOfficialExamples(t *testing.T) {
	v := newValidator(t)
	cases := []struct {
		kind contracts.Kind
		name string
		new  func() any
	}{
		{contracts.KindAudioObservation, "audio-observation.example.json", func() any { return &contracts.AudioObservation{} }},
		{contracts.KindVisualObservation, "visual-observation.example.json", func() any { return &contracts.VisualObservation{} }},
		{contracts.KindContextEventV1, "context-event-v1.example.json", func() any { return &contracts.ContextEventV1{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readExample(t, tc.name)
			if err := v.ValidateJSON(tc.kind, raw); err != nil {
				t.Fatalf("schema: %v", err)
			}
			dest := tc.new()
			if err := json.Unmarshal(raw, dest); err != nil {
				t.Fatal(err)
			}
			if err := contracts.Validate(dest); err != nil {
				t.Fatalf("domain: %v", err)
			}
			out, err := json.Marshal(dest)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.ValidateJSON(tc.kind, out); err != nil {
				t.Fatalf("round-trip schema: %v\n%s", err, out)
			}
		})
	}
}

func TestOfficialExamplesEvidence(t *testing.T) {
	audioRaw := readExample(t, "audio-observation.example.json")
	visualRaw := readExample(t, "visual-observation.example.json")
	eventRaw := readExample(t, "context-event-v1.example.json")

	var audio contracts.AudioObservation
	var visual contracts.VisualObservation
	var event contracts.ContextEventV1
	if err := json.Unmarshal(audioRaw, &audio); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(visualRaw, &visual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(eventRaw, &event); err != nil {
		t.Fatal(err)
	}
	catalog, err := contracts.NewCatalog([]contracts.AudioObservation{audio}, []contracts.VisualObservation{visual})
	if err != nil {
		t.Fatal(err)
	}
	if err := event.ValidateEvidence(catalog); err != nil {
		t.Fatal(err)
	}
}

func TestValidatorConcurrent(t *testing.T) {
	v := newValidator(t)
	raw := readExample(t, "context-event-v1.example.json")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.ValidateJSON(contracts.KindContextEventV1, raw); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "specs", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
