package providers

import (
	"reflect"
	"testing"

	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/audio/whispercpp"
)

func TestDefaultRegistersBuiltInAdapters(t *testing.T) {
	r := Default()
	if got := r.Names(); !reflect.DeepEqual(got, []string{whispercpp.ProviderName}) {
		t.Fatalf("names: %v", got)
	}
	transcriber, err := r.New(whispercpp.ProviderName, audio.Options{whispercpp.OptionModel: "ggml-tiny.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcriber.(*whispercpp.Adapter); !ok {
		t.Fatalf("transcriber: %T", transcriber)
	}
	if Default() == r {
		t.Fatal("registries are shared")
	}
}
