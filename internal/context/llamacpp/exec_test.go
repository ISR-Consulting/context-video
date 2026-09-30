package llamacpp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestChildEnvStripsLlamaArgs(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "LLAMA_ARG_HF_REPO=x/y", "HOME=/h", "LLAMA_ARG_MODEL=m"})
	if !reflect.DeepEqual(got, []string{"PATH=/bin", "HOME=/h"}) {
		t.Fatal(got)
	}
}

func TestBuffers(t *testing.T) {
	var lb limitBuffer
	chunk := bytes.Repeat([]byte("a"), MaxStdout-1)
	_, _ = lb.Write(chunk)
	_, _ = lb.Write([]byte("bc"))
	if !lb.overflow || len(lb.data) != MaxStdout {
		t.Fatalf("overflow %t len %d", lb.overflow, len(lb.data))
	}
	var tb tailBuffer
	_, _ = tb.Write(bytes.Repeat([]byte("x"), maxStderr))
	_, _ = tb.Write([]byte("END"))
	if len(tb.Bytes()) != maxStderr || !strings.HasSuffix(string(tb.Bytes()), "END") {
		t.Fatalf("tail %d", len(tb.Bytes()))
	}
}

// TestHelperProcess is the child process of the ExecRunner tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("LLAMACPP_HELPER") != "1" {
		t.Skip("helper process")
	}
	switch os.Args[len(os.Args)-1] {
	case "env":
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "LLAMA_ARG_") {
				os.Stdout.WriteString(kv + "\n")
			}
		}
		os.Stdout.WriteString("ok")
	case "fail":
		os.Stderr.WriteString("boom")
		os.Exit(3)
	}
	os.Exit(0)
}

func TestExecRunner(t *testing.T) {
	t.Setenv("LLAMACPP_HELPER", "1")
	t.Setenv("LLAMA_ARG_HF_REPO", "org/model")
	args := func(mode string) []string { return []string{"-test.run=TestHelperProcess", "--", mode} }
	stdout, _, err := ExecRunner{}.Run(context.Background(), os.Args[0], args("env"))
	if err != nil || string(stdout) != "ok" {
		t.Fatalf("stdout %q err %v", stdout, err)
	}
	_, stderr, err := ExecRunner{}.Run(context.Background(), os.Args[0], args("fail"))
	if err == nil || !strings.Contains(string(stderr), "boom") {
		t.Fatalf("stderr %q err %v", stderr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (ExecRunner{}).Run(ctx, os.Args[0], args("env")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
