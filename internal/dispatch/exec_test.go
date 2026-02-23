package dispatch

import (
	"errors"
	"testing"
)

func TestExecBinary_DelegatesToSyscallExec(t *testing.T) {
	original := syscallExec
	t.Cleanup(func() { syscallExec = original })

	wantErr := errors.New("exec failed")
	called := false
	syscallExec = func(path string, args []string, env []string) error {
		called = true
		if path != "/bin/example" {
			t.Fatalf("expected path /bin/example, got %q", path)
		}
		if len(args) != 2 || args[0] != "example" || args[1] != "--flag" {
			t.Fatalf("unexpected args: %#v", args)
		}
		if len(env) != 1 || env[0] != "KEY=VALUE" {
			t.Fatalf("unexpected env: %#v", env)
		}
		return wantErr
	}

	err := execBinary("/bin/example", []string{"example", "--flag"}, []string{"KEY=VALUE"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if !called {
		t.Fatal("expected syscallExec to be called")
	}
}
