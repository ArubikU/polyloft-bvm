package main_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

func benchProgram(b *testing.B, path string) {
	var sink bytes.Buffer
	program, registry, err := modules.Prepare(path, &sink)
	if err != nil {
		b.Fatal(err)
	}
	if err := sema.Check(program, registry); err != nil {
		b.Fatal(err)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		machine := vm.NewWithRegistry(io.Discard, registry)
		if _, err := machine.Run(fn); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib(b *testing.B) {
	benchProgram(b, filepath.Join("testdata", "programs", "bench_fib.pf"))
}

func BenchmarkFloat(b *testing.B) {
	benchProgram(b, filepath.Join("testdata", "programs", "bench_float.pf"))
}

func BenchmarkPoly(b *testing.B) {
	benchProgram(b, filepath.Join("testdata", "programs", "bench_poly.pf"))
}

func BenchmarkArray(b *testing.B) {
	benchProgram(b, filepath.Join("testdata", "programs", "bench_array.pf"))
}

func BenchmarkSort(b *testing.B) {
	benchProgram(b, filepath.Join("testdata", "programs", "bench_sort.pf"))
}
