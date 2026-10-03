package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

// TestRegressionPrograms runs every testdata/regress/*.pf and compares its output with the
// .expected file next to it. The expected outputs were produced by the interpreter before the
// performance work of OPTIMIZATION.md section 10 (frame pooling, fused opcodes, string fast paths,
// single-allocation instances), so any behavioural drift in those paths fails here.
func TestRegressionPrograms(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "regress", "*.pf"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no regression programs found: %v", err)
	}
	for _, path := range files {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			want, err := os.ReadFile(strings.TrimSuffix(path, ".pf") + ".expected")
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			program, registry, err := modules.Prepare(path, &out)
			if err != nil {
				t.Fatal(err)
			}
			if err := sema.Check(program, registry); err != nil {
				t.Fatal(err)
			}
			fn, err := compiler.CompileWithRegistry(program, registry)
			if err != nil {
				t.Fatal(err)
			}
			machine := vm.NewWithRegistry(&out, registry)
			if _, err := machine.Run(fn); err != nil {
				t.Fatal(err)
			}
			norm := func(b []byte) string { return strings.ReplaceAll(strings.TrimSpace(string(b)), "\r\n", "\n") }
			if got := norm(out.Bytes()); got != norm(want) {
				t.Fatalf("output differs from %s.expected\n--- got ---\n%s\n--- want ---\n%s", path, got, norm(want))
			}
		})
	}
}
