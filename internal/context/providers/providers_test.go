package providers

import (
	"reflect"
	"testing"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/context/llamacpp"
	"github.com/ISR-Consulting/context-video/internal/context/openai"
)

func TestDefaultRegistersBuiltInAdapters(t *testing.T) {
	r := Default()
	if got := r.Names(); !reflect.DeepEqual(got, []string{llamacpp.ProviderName, openai.ProviderName}) {
		t.Fatalf("names: %v", got)
	}
	if _, err := r.New(llamacpp.ProviderName, contextcore.Options{"model": "/models/m.gguf"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "test")
	oai, err := r.New(openai.ProviderName, contextcore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := oai.(*openai.Adapter); !ok {
		t.Fatalf("openai: %T", oai)
	}
}
