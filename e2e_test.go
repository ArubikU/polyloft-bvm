package main_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/lexer"
	"github.com/ArubikU/polyloft-bvm/internal/parser"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

func TestEndToEndDemoSlice(t *testing.T) {
	source := `def add(a, b):
    return a + b
end

let total = 0
for i in range(0, 6) where i < 5:
    total = total + i
end

if add(total, 1) > 10:
    println("ok")
else:
    println("no")
end

println(total)

let started = Sys.time()
Sys.sleep(1)
println(Sys.time() >= started)
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}

	if got, want := out.String(), "ok\n10\ntrue\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestTypeCheckerRejectsWrongAnnotatedAssignment(t *testing.T) {
	source := `let x: Number = "oops"`
	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &bytes.Buffer{})
	if err := sema.Check(program, registry); err == nil {
		t.Fatalf("expected type checker to reject invalid assignment")
	}
}

func TestEndToEndClassExecution(t *testing.T) {
	source := `class Persona:
    nombre: String

    Persona(n: String):
        this.nombre = n
    end

    def mostrar():
        return this.nombre
    end
end

let persona = new Persona("Ana")
println(persona.nombre)
println(persona.mostrar())
persona.nombre = "Bea"
println(persona.nombre)
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}

	if got, want := out.String(), "Ana\nAna\nBea\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestClassMethodReturnTypeAndInference(t *testing.T) {
	source := `class Worker:
    factor: Number

    Worker(f: Number):
        this.factor = f
    end

    def run(limit: Number) -> Number:
        let acc = 0
        for i in range(0, limit):
            acc = acc + (i * this.factor)
        end
        return acc
    end
end

let worker = new Worker(3)
println(worker.run(5))
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "30\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestClassMethodReturnTypeEnablesNumericSpecialization(t *testing.T) {
	source := `class Worker:
    factor: Number

    Worker(f: Number):
        this.factor = f
    end

    def run(limit: Number) -> Number:
        let acc = 0
        for i in range(0, limit):
            acc = acc + (i * this.factor)
        end
        return acc
    end
end

let worker = new Worker(3)
let total = 0
total = total + worker.run(5)
println(total)
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "30\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
	if disassembly := fn.Chunk.Disassemble(fn.Name); !strings.Contains(disassembly, "ADD_NUM") {
		t.Fatalf("expected specialized numeric add in script chunk, got:\n%s", disassembly)
	}
}

func TestStaticFieldsAndMethods(t *testing.T) {
	source := `class MathHelper:
    static let PI: Number = 3.14159
    static var count: Number = 0

    static def square(x: Number) -> Number:
        return x * x
    end

    static def tick() -> Number:
        MathHelper.count = MathHelper.count + 1
        return MathHelper.count
    end
end

println(MathHelper.PI)
println(MathHelper.square(5))
println(MathHelper.tick())
println(MathHelper.tick())
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "3.14159\n25\n1\n2\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestLogicalOperatorsAndModuloShortCircuit(t *testing.T) {
	source := `let count = 0

def mark() -> Bool:
    count = count + 1
    return true
end

println(10 % 3)
println(false && mark())
println(true || mark())
println(count)
println(true && mark())
println(count)
`

	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var out bytes.Buffer
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &out)
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "1\nfalse\ntrue\n0\ntrue\n1\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}
