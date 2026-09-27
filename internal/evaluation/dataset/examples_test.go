package dataset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialExamplesValidateAndRoundTrip(t *testing.T) {
	specs := os.DirFS(filepath.Join("..", "..", "..", "specs"))
	loader, err := NewLoader(specs, specs)
	if err != nil {
		t.Fatal(err)
	}

	manifestRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "examples", "golden-dataset-manifest-v1.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := loader.LoadManifest("examples/golden-dataset-manifest-v1.example.json")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	manifestRoundTrip, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(loader.manifestSchema, "manifest round trip", manifestRoundTrip); err != nil {
		t.Fatal(err)
	}
	if len(manifestRaw) == 0 {
		t.Fatal("manifest example is empty")
	}

	groundTruth, err := loader.LoadGroundTruth("examples/golden-ground-truth-v1.example.json")
	if err != nil {
		t.Fatalf("ground truth: %v", err)
	}
	groundTruthRoundTrip, err := json.Marshal(groundTruth)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(loader.groundTruthSchema, "ground truth round trip", groundTruthRoundTrip); err != nil {
		t.Fatal(err)
	}
}

func TestClosedGroundTruthSchemaRejectsCommercialField(t *testing.T) {
	loader, err := NewLoader(
		os.DirFS(filepath.Join("testdata", "invalid")),
		os.DirFS(filepath.Join("..", "..", "..", "specs")),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loader.LoadGroundTruth("commercial-ground-truth.json")
	if err == nil {
		t.Fatal("expected commercial field to be rejected")
	}
	assertErrorContains(t, err, LayerSchema, "productId")
}
