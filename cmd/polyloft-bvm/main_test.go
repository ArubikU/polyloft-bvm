package main

import (
	"testing"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
)

func TestParseRunArgs_Default(t *testing.T) {
	path, opts, err := parseRunArgs([]string{"program.pf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "program.pf" {
		t.Fatalf("unexpected path: %s", path)
	}
	if opts.jitThreshold != nil {
		t.Fatalf("expected nil jit threshold, got %v", *opts.jitThreshold)
	}
	if opts.jitLog {
		t.Fatalf("expected jit log disabled")
	}
}

func TestParseRunArgs_JITFlags(t *testing.T) {
	path, opts, err := parseRunArgs([]string{"--jit", "--jit-log", "program.pf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "program.pf" {
		t.Fatalf("unexpected path: %s", path)
	}
	if opts.jitThreshold == nil || *opts.jitThreshold != 1 {
		t.Fatalf("expected jit threshold 1, got %+v", opts.jitThreshold)
	}
	if !opts.jitLog {
		t.Fatalf("expected jit log enabled")
	}
}

func TestParseRunArgs_ExplicitThresholdWins(t *testing.T) {
	_, opts, err := parseRunArgs([]string{"--jit", "--jit-threshold", "9", "program.pf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.jitThreshold == nil || *opts.jitThreshold != 9 {
		t.Fatalf("expected jit threshold 9, got %+v", opts.jitThreshold)
	}
}

func TestParseRunArgs_InvalidThreshold(t *testing.T) {
	if _, _, err := parseRunArgs([]string{"--jit-threshold", "0", "program.pf"}); err == nil {
		t.Fatalf("expected error for invalid threshold")
	}
}

func TestShouldEnableJITForFunction_ScriptWithoutNestedFunctions(t *testing.T) {
	threshold := 1
	fn := &bytecode.Function{Name: "<script>", Chunk: &bytecode.Chunk{Constants: []any{"text", int64(1)}}}
	if !shouldEnableJITForFunction(fn, runOptions{jitThreshold: &threshold}) {
		t.Fatalf("expected pure top-level script to keep JIT enabled")
	}
}

func TestShouldEnableJITForFunction_ScriptWithNestedFunction(t *testing.T) {
	threshold := 1
	child := &bytecode.Function{Name: "helper", Chunk: &bytecode.Chunk{}}
	fn := &bytecode.Function{Name: "<script>", Chunk: &bytecode.Chunk{Constants: []any{child}}}
	if !shouldEnableJITForFunction(fn, runOptions{jitThreshold: &threshold}) {
		t.Fatalf("expected script with nested bytecode functions to keep JIT enabled")
	}
}
