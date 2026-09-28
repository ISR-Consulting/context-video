package providers

import (
	"reflect"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/internal/vision/llamamtmd"
)

func TestDefaultRegistersBuiltInAdapters(t *testing.T) {
	r := Default()
	if got := r.Names(); !reflect.DeepEqual(got, []string{llamamtmd.ProviderName}) {
		t.Fatalf("names: %v", got)
	}
	analyzer, err := r.New(llamamtmd.ProviderName, vision.Options{llamamtmd.OptionModel: "vlm.gguf", llamamtmd.OptionMMProj: "mmproj.gguf"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := analyzer.(*llamamtmd.Adapter); !ok {
		t.Fatalf("analyzer: %T", analyzer)
	}
	if Default() == r {
		t.Fatal("registries are shared")
	}
}
