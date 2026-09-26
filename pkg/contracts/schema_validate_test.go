package contracts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestSchemasCompileAndFixtures(t *testing.T) {
	v := newValidator(t)
	for _, dir := range []string{"valid", "invalid"} {
		entries, err := os.ReadDir(filepath.Join("testdata", "schema", dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatalf("no fixtures in %s", dir)
		}
		for _, entry := range entries {
			name := entry.Name()
			kind, ok := kindFromName(name)
			if !ok {
				t.Fatalf("cannot map fixture %s to a contract kind", name)
			}
			raw, err := os.ReadFile(filepath.Join("testdata", "schema", dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = v.ValidateJSON(kind, raw)
			if dir == "valid" && err != nil {
				t.Errorf("schema %s: %v", name, err)
			}
			if dir == "invalid" && err == nil {
				t.Errorf("schema %s: expected an error", name)
			}
			if dir == "invalid" && err != nil && !strings.Contains(err.Error(), "schema:") {
				t.Errorf("schema %s: error %q is not a schema error", name, err)
			}
		}
	}
}

func TestUnknownKind(t *testing.T) {
	v := newValidator(t)
	err := v.ValidateJSON(contracts.Kind("nope"), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unknown contract kind") {
		t.Fatalf("got %v", err)
	}
}

func TestSchemaValidDomainInvalidWindow(t *testing.T) {
	v := newValidator(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "schema", "valid", "context-equal-window.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateJSON(contracts.KindContextEventV1, raw); err != nil {
		t.Fatalf("schema: %v", err)
	}
	var event contracts.ContextEventV1
	if err := unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	err = contracts.Validate(event)
	if err == nil || !strings.Contains(err.Error(), "endMs must be > startMs") {
		t.Fatalf("domain: %v", err)
	}
}

func TestSchemaAcceptsCommercialKeysInOpenObjects(t *testing.T) {
	v := newValidator(t)
	cases := []struct {
		name string
		kind contracts.Kind
	}{
		{"media-product-id.json", contracts.KindMediaSegment},
		{"experiment-product-id.json", contracts.KindExperimentResult},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(filepath.Join("testdata", "schema", "valid", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := v.ValidateJSON(tc.kind, raw); err != nil {
			t.Errorf("%s schema: %v", tc.name, err)
		}
	}
}

func kindFromName(name string) (contracts.Kind, bool) {
	switch {
	case strings.HasPrefix(name, "audio-"):
		return contracts.KindAudioObservation, true
	case strings.HasPrefix(name, "visual-"):
		return contracts.KindVisualObservation, true
	case strings.HasPrefix(name, "context-"):
		return contracts.KindContextEventV1, true
	case strings.HasPrefix(name, "media-"):
		return contracts.KindMediaSegment, true
	case strings.HasPrefix(name, "experiment-"):
		return contracts.KindExperimentResult, true
	default:
		return "", false
	}
}
