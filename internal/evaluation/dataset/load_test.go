package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

func TestLoadValidDatasetPreservesLiveVODAndOrder(t *testing.T) {
	loader, err := NewLoader(
		osDirFS(t, filepath.Join("testdata", "valid")),
		osDirFS(t, filepath.Join("..", "..", "..", "specs")),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loader.LoadDataset("manifests/poc-golden-v1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TestCases) != 2 {
		t.Fatalf("test cases: %d", len(got.TestCases))
	}
	if got.TestCases[0].TestCase.Content.ContentType != contracts.ContentTypeLive {
		t.Fatalf("first type: %q", got.TestCases[0].TestCase.Content.ContentType)
	}
	if got.TestCases[1].TestCase.Content.ContentType != contracts.ContentTypeVOD {
		t.Fatalf("second type: %q", got.TestCases[1].TestCase.Content.ContentType)
	}
	if got.TestCases[0].GroundTruth.Annotations[0].Window != (contracts.TimeWindow{StartMs: 0, EndMs: 10000}) {
		t.Fatalf("boundary window: %#v", got.TestCases[0].GroundTruth.Annotations[0].Window)
	}
}

func TestMalformedAndSchemaInvalidManifest(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "malformed", raw: `{"schemaVersion":`},
		{name: "schema invalid", raw: `{}`},
		{name: "unknown property", raw: `{"schemaVersion":"1.0","unexpected":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := newMapLoader(t, fstest.MapFS{
				"manifest.json": &fstest.MapFile{Data: []byte(tc.raw)},
			})
			_, err := loader.LoadManifest("manifest.json")
			assertErrorContains(t, err, LayerSchema, "")
		})
	}
}

func TestMissingAndMismatchedGroundTruthReferences(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		fsys := validMapFS(t)
		delete(fsys, "ground-truth/test.json")
		_, err := newMapLoader(t, fsys).LoadDataset("manifest.json")
		assertErrorContains(t, err, LayerReference, "read ground truth")
	})

	cases := []struct {
		name   string
		mutate func(*GroundTruth)
		want   string
	}{
		{
			name:   "test case id",
			mutate: func(value *GroundTruth) { value.TestCaseID = "other" },
			want:   "testCaseId",
		},
		{
			name:   "content id",
			mutate: func(value *GroundTruth) { value.Content.ContentID = "other" },
			want:   "content.contentId",
		},
		{
			name:   "content type",
			mutate: func(value *GroundTruth) { value.Content.ContentType = contracts.ContentTypeVOD },
			want:   "content.contentType",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := validMapFS(t)
			groundTruth := validGroundTruth()
			tc.mutate(&groundTruth)
			fsys["ground-truth/test.json"] = jsonMapFile(t, groundTruth)
			_, err := newMapLoader(t, fsys).LoadDataset("manifest.json")
			assertErrorContains(t, err, LayerReference, tc.want)
		})
	}
}

func TestGroundTruthCannotExceedMediaDuration(t *testing.T) {
	fsys := validMapFS(t)
	groundTruth := validGroundTruth()
	groundTruth.Annotations[0].Window.EndMs = 1001
	fsys["ground-truth/test.json"] = jsonMapFile(t, groundTruth)
	_, err := newMapLoader(t, fsys).LoadDataset("manifest.json")
	assertErrorContains(t, err, LayerDomain, "durationMs 1000")
}

func TestDatasetPathsCannotEscape(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{
			name:   "absolute ground truth",
			mutate: func(value *Manifest) { value.TestCases[0].GroundTruthPath = "/outside.json" },
			want:   "groundTruthPath",
		},
		{
			name:   "escaping ground truth",
			mutate: func(value *Manifest) { value.TestCases[0].GroundTruthPath = "../outside.json" },
			want:   "groundTruthPath",
		},
		{
			name:   "absolute media",
			mutate: func(value *Manifest) { value.TestCases[0].Media.URI = "/outside.media" },
			want:   "media.uri",
		},
		{
			name:   "escaping media",
			mutate: func(value *Manifest) { value.TestCases[0].Media.URI = "../outside.media" },
			want:   "media.uri",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := validMapFS(t)
			manifest := validManifest()
			tc.mutate(&manifest)
			fsys["manifest.json"] = jsonMapFile(t, manifest)
			_, err := newMapLoader(t, fsys).LoadManifest("manifest.json")
			assertErrorContains(t, err, LayerDomain, tc.want)
		})
	}
}

func TestFixtureExistenceAndDigest(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		fsys := validMapFS(t)
		delete(fsys, "media/test.fixture")
		_, err := newMapLoader(t, fsys).LoadDataset("manifest.json")
		assertErrorContains(t, err, LayerReference, "read media")
	})
	t.Run("digest mismatch", func(t *testing.T) {
		fsys := validMapFS(t)
		fsys["media/test.fixture"] = &fstest.MapFile{Data: []byte("changed")}
		_, err := newMapLoader(t, fsys).LoadDataset("manifest.json")
		assertErrorContains(t, err, LayerReference, "digest mismatch")
	})
}

func TestControlledSourceDoesNotResolveURI(t *testing.T) {
	fsys := validMapFS(t)
	delete(fsys, "media/test.fixture")
	manifest := validManifest()
	manifest.TestCases[0].Media.Kind = MediaKindControlledSource
	manifest.TestCases[0].Media.URI = "controlled://approved/not-present"
	fsys["manifest.json"] = jsonMapFile(t, manifest)
	if _, err := newMapLoader(t, fsys).LoadDataset("manifest.json"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadingIsDeterministic(t *testing.T) {
	loader := newMapLoader(t, validMapFS(t))
	first, err := loader.LoadDataset("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := loader.LoadDataset("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated loads differ:\n%#v\n%#v", first, second)
	}

	fsys := validMapFS(t)
	groundTruth := validGroundTruth()
	groundTruth.Annotations[0].Expected.Required.Topics = []string{"football", "football"}
	fsys["ground-truth/test.json"] = jsonMapFile(t, groundTruth)
	invalidLoader := newMapLoader(t, fsys)
	_, firstErr := invalidLoader.LoadDataset("manifest.json")
	_, secondErr := invalidLoader.LoadDataset("manifest.json")
	if firstErr == nil || secondErr == nil || firstErr.Error() != secondErr.Error() {
		t.Fatalf("errors are not deterministic: %v / %v", firstErr, secondErr)
	}
}

func newMapLoader(t *testing.T, datasetFS fstest.MapFS) *Loader {
	t.Helper()
	loader, err := NewLoader(datasetFS, osDirFS(t, filepath.Join("..", "..", "..", "specs")))
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

func validMapFS(t *testing.T) fstest.MapFS {
	t.Helper()
	media := []byte("fixture")
	sum := sha256.Sum256(media)
	manifest := validManifest()
	manifest.TestCases[0].Media.SHA256 = hex.EncodeToString(sum[:])
	return fstest.MapFS{
		"manifest.json":          jsonMapFile(t, manifest),
		"ground-truth/test.json": jsonMapFile(t, validGroundTruth()),
		"media/test.fixture":     &fstest.MapFile{Data: media},
	}
}

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:               SchemaVersion,
		DatasetID:                   "test-dataset",
		DatasetVersion:              "1.0",
		Name:                        "Test Dataset",
		AnnotationGuidelinesVersion: "1.0",
		TestCases: []TestCase{{
			TestCaseID: "test-case",
			Scenario:   ScenarioFootballSportsApparel,
			Content: contracts.ContentRef{
				ContentID:   "live-test",
				ContentType: contracts.ContentTypeLive,
			},
			Media: MediaReference{
				Kind:       MediaKindFixture,
				URI:        "media/test.fixture",
				SHA256:     strings.Repeat("0", 64),
				DurationMs: 1000,
			},
			GroundTruthPath: "ground-truth/test.json",
		}},
	}
}

func validGroundTruth() GroundTruth {
	return GroundTruth{
		SchemaVersion: SchemaVersion,
		TestCaseID:    "test-case",
		Content: contracts.ContentRef{
			ContentID:   "live-test",
			ContentType: contracts.ContentTypeLive,
		},
		Annotations: []GroundTruthAnnotation{{
			AnnotationID: "annotation-1",
			Window:       contracts.TimeWindow{StartMs: 0, EndMs: 1000},
			Expected: ExpectedContext{
				Required: ExpectedSet{Topics: []string{"football"}},
			},
		}},
	}
}

func jsonMapFile(t *testing.T, value any) *fstest.MapFile {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return &fstest.MapFile{Data: data}
}

func osDirFS(t *testing.T, root string) fs.FS {
	t.Helper()
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory %s: %v", root, err)
	}
	return os.DirFS(root)
}

func assertErrorContains(t *testing.T, err error, layer, text string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), layer+":") || (text != "" && !strings.Contains(err.Error(), text)) {
		t.Fatalf("error %q does not contain layer %q and text %q", err, layer, text)
	}
}
