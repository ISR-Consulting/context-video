package llamamtmd

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func shell(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	return sh
}

func TestExecRunnerCapturesStdoutAndStderr(t *testing.T) {
	sh := shell(t)
	stdout, stderr, err := ExecRunner{}.Run(context.Background(), sh, []string{"-c", `printf '{"observations":[]}'; echo log >&2`})
	if err != nil || string(stdout) != `{"observations":[]}` || strings.TrimSpace(string(stderr)) != "log" {
		t.Fatalf("stdout %q stderr %q err %v", stdout, stderr, err)
	}
	_, stderr, err = ExecRunner{}.Run(context.Background(), sh, []string{"-c", "echo oops >&2; exit 3"})
	if err == nil || strings.TrimSpace(string(stderr)) != "oops" {
		t.Fatalf("stderr %q err %v", stderr, err)
	}
}

func TestExecRunnerStripsLlamaArgEnvironment(t *testing.T) {
	sh := shell(t)
	t.Setenv("LLAMA_ARG_HF_REPO", "someone/model")
	t.Setenv("CONTEXT_VIDEO_KEEP", "kept")
	stdout, _, err := ExecRunner{}.Run(context.Background(), sh, []string{"-c", `printf '%s|%s' "${LLAMA_ARG_HF_REPO:-unset}" "$CONTEXT_VIDEO_KEEP"`})
	if err != nil || string(stdout) != "unset|kept" {
		t.Fatalf("stdout %q err %v", stdout, err)
	}
	if got := childEnv([]string{"A=1", "LLAMA_ARG_MODEL=x", "LLAMA_CACHE=y"}); !reflect.DeepEqual(got, []string{"A=1", "LLAMA_CACHE=y"}) {
		t.Fatalf("env: %v", got)
	}
}

func TestExecRunnerRejectsOversizedStdout(t *testing.T) {
	sh := shell(t)
	_, _, err := ExecRunner{}.Run(context.Background(), sh, []string{"-c", "head -c 1048577 /dev/zero"})
	if !errors.Is(err, ErrStdoutTooLarge) {
		t.Fatalf("err: %v", err)
	}
}

func TestExecRunnerCancellation(t *testing.T) {
	sh := shell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (ExecRunner{}).Run(ctx, sh, []string{"-c", "sleep 5"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run: %v", err)
	}
}

func TestBuffers(t *testing.T) {
	var tail tailBuffer
	_, _ = tail.Write([]byte(strings.Repeat("a", maxStderr)))
	_, _ = tail.Write([]byte("tail"))
	if got := string(tail.Bytes()); len(got) != maxStderr || !strings.HasSuffix(got, "tail") {
		t.Fatalf("tail len %d", len(got))
	}
	var limit limitBuffer
	_, _ = limit.Write([]byte(strings.Repeat("a", MaxStdout)))
	if limit.overflow {
		t.Fatal("overflow at exactly the limit")
	}
	if n, err := limit.Write([]byte("b")); n != 1 || err != nil || !limit.overflow || len(limit.data) != MaxStdout {
		t.Fatalf("overflow not recorded: n=%d err=%v len=%d", n, err, len(limit.data))
	}
}
