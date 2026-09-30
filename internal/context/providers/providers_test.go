package providers

import (
	"reflect"
	"testing"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/context/llamacpp"
)

func TestDefaultRegistersLlamaCpp(t *testing.T) {
	r := Default()
	if !reflect.DeepEqual(r.Names(), []string{llamacpp.ProviderName}) {
		t.Fatal(r.Names())
	}
	if _, err := r.New(llamacpp.ProviderName, contextcore.Options{"model": "/models/m.gguf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.New(llamacpp.ProviderName, nil); err == nil {
		t.Fatal("missing model accepted")
	}
}
