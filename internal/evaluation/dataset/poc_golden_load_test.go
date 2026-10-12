package dataset_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
)

func TestCommittedPocGoldenLoads(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// test runs from package dir; walk up to module root
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("go.mod not found")
		}
		root = parent
	}
	l, err := dataset.NewLoader(os.DirFS(filepath.Join(root, "dataset")), os.DirFS(filepath.Join(root, "specs")))
	if err != nil {
		t.Fatal(err)
	}
	ds, err := l.LoadDataset("manifests/poc-golden-v1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.TestCases) != 4 {
		t.Fatalf("cases=%d", len(ds.TestCases))
	}
}
