package main_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/lexer"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
	"github.com/ArubikU/polyloft-bvm/internal/parser"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/value"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

func runPreparedFile(t *testing.T, filePath string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	program, registry, err := modules.Prepare(filePath, &out)
	if err != nil {
		return "", err
	}
	if err := sema.Check(program, registry); err != nil {
		return "", err
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		return "", err
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		return "", err
	}
	return out.String(), nil
}

func TestNewArraySizeAndInitializer(t *testing.T) {
	// size specified must match initializer count, and nested empty works
	source := `
let ok: int[] = new int[3]{0,1,2}
let empty: array<array<int>> = new int[][]{}
`
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
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
}

func TestNumberAssignableToInt(t *testing.T) {
	source := `
let n: number = 3.4
let i: int = n
let f: float = n
let acc: int = 0
acc += n
`
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
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
}

// helper that compiles a real file and inspects its constant pool for nils
func TestDemo2ConstantPool(t *testing.T) {
	path := "testdata/programs/demo2.pf"
	program, registry, err := modules.Prepare(path, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("sema failed: %v", err)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	for idx, c := range fn.Chunk.Constants {
		var scan func(any, string)
		scan = func(c any, path string) {
			if c == nil {
				t.Fatalf("nil at %s", path)
			}
			rv := reflect.ValueOf(c)
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				for i := 0; i < rv.Len(); i++ {
					elem := rv.Index(i).Interface()
					scan(elem, fmt.Sprintf("%s[%d]", path, i))
				}
			case reflect.Map:
				for _, key := range rv.MapKeys() {
					val := rv.MapIndex(key).Interface()
					scan(val, fmt.Sprintf("%s[%v]", path, key))
				}
			}
		}
		scan(c, fmt.Sprintf("constant[%d]", idx))
	}
}

func TestCompileProducesPfx(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join("testdata", "programs", "demo2.pf")
	tgt := filepath.Join(tmp, "out.pfx")
	// run compile command programmatically
	program, registry, err := modules.Prepare(src, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("sema failed: %v", err)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	out, err := os.Create(tgt)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	_, err = out.WriteString(fn.Chunk.Disassemble(fn.Name))
	out.Close()
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	info, err := os.Stat(tgt)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("empty pfx file")
	}
}

func TestLinkedListBehavesLikeList(t *testing.T) {
	source := `
import polyloft.collections { List, LinkedList }
let ll: List = LinkedList()
ll.add("a")
ll.add("b")
println(ll.size())
println(ll.contains("a"))
println(ll.remove("a"))
println(ll.contains("a"))
println(ll.asArray()[0])
`
	// write to a temporary file so that modules.Prepare can resolve imports
	tmp := filepath.Join(os.TempDir(), "linkedlist_test.pf")
	if err := os.WriteFile(tmp, []byte(source), 0644); err != nil {
		t.Fatalf("failed to write temp source: %v", err)
	}
	out, err := runPreparedFile(t, tmp)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if out != "2\ntrue\ntrue\nfalse\nb\n" {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", "2\ntrue\ntrue\nfalse\nb\n", out)
	}
}

// Regression: compound assignment (+=, -=, *=) on a closure-captured local
// must write through the capture cell, not just the frame slot. Previously
// OpAddToLocal/SubToLocal/MulToLocal bypassed hasCells, so closures read
// stale values (this program returned 8 instead of 20).
func TestClosureSeesCompoundAssignOnCapturedLocal(t *testing.T) {
	out, err := runPreparedFile(t, filepath.Join("testdata", "programs", "test_closure_compound.pf"))
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if out != "total=20\n" {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", "total=20\n", out)
	}
}

// Regression: the value stack is pre-allocated (4096 slots) and push() has no
// bounds check; ensureStackHeadroom in acquireFrame must grow it for deep
// recursion instead of panicking with an out-of-range index.
func TestDeepRecursionGrowsValueStack(t *testing.T) {
	out, err := runPreparedFile(t, filepath.Join("testdata", "programs", "test_deep_recursion.pf"))
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if out != "depth=20000\n" {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", "depth=20000\n", out)
	}
}

// Array comprehensions: [expr for var in iterable] over ranges and arrays,
// including use inside function bodies.
func TestArrayComprehension(t *testing.T) {
	out, err := runPreparedFile(t, filepath.Join("testdata", "programs", "test_comprehension.pf"))
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	want := "sq=16\nlen=5\nd0=20\nd2=60\na0=4\na3=1\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestNewArrayTooManyValues(t *testing.T) {
	source := `
let bad: int[] = new int[2]{0,1,2}
`
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
		t.Fatalf("expected type checker to reject initializer longer than size")
	}
}

func TestArraySyntaxVariants(t *testing.T) {
	// verify both generic and bracket syntax work and can be nested
	source := `
let a: array<int> = [1, 2]
let b: int[] = [3, 4]
let c: array<array<string>> = [["x"]]
let d: string[][] = [["y"]]
// make sure the compiler cares about the element types
let sum: int = a[0] + a[1] + b[0] + b[1]
println(sum)
println(c[0][0])
println(d[0][0])
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
	if got, want := out.String(), "10\nx\ny\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

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
	source := `let x: number = "oops"`
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

func TestTypeCheckerRejectsFinalReassignment(t *testing.T) {
	source := `final answer = 41
answer = 42`
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
	err = sema.Check(program, registry)
	if err == nil {
		t.Fatalf("expected type checker to reject final reassignment")
	}
	if !strings.Contains(err.Error(), "immutable variable answer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFinalInliningElidesCaptureLoad(t *testing.T) {
	source := `def make() -> number:
    final base = 40
    let answer = () => base + 2
    return answer()
end

println(make())
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
	var makeFn *bytecode.Function
	for _, constant := range fn.Chunk.Constants {
		candidate, ok := constant.(*bytecode.Function)
		if ok && candidate.Name == "make" {
			makeFn = candidate
			break
		}
	}
	if makeFn == nil {
		t.Fatalf("expected make function constant")
	}
	var lambdaFn *bytecode.Function
	for _, constant := range makeFn.Chunk.Constants {
		candidate, ok := constant.(*bytecode.Function)
		if ok && strings.HasPrefix(candidate.Name, "<lambda:") {
			lambdaFn = candidate
			break
		}
	}
	if lambdaFn == nil {
		t.Fatalf("expected lambda function constant")
	}
	if disassembly := lambdaFn.Chunk.Disassemble(lambdaFn.Name); strings.Contains(disassembly, "GET_CAPTURE") {
		t.Fatalf("expected final capture to be inlined, got:\n%s", disassembly)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "42\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestTypeCheckerRejectsWrongAnnotatedArrayElementAssignment(t *testing.T) {
	source := `let values: array<number> = ["oops"]`
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
		t.Fatalf("expected type checker to reject invalid array assignment")
	}
}

func TestUnionAliasesReachBytecodeAndRuntimeChecks(t *testing.T) {
	source := `type Scalar = number | string

def take(items: array<Scalar>) -> int:
    return len(items)
end

let payload: any = ["a", 1]
println(take(payload))
let inline: array<Scalar> = ["z", 2]
println(inline[0])
println(inline[1])
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
	var takeFn *bytecode.Function
	for _, constant := range fn.Chunk.Constants {
		candidate, ok := constant.(*bytecode.Function)
		if ok && candidate.Name == "take" {
			takeFn = candidate
			break
		}
	}
	if takeFn == nil {
		t.Fatalf("expected take function constant")
	}
	if got, want := takeFn.ParamTypes[0], "array<number | String>"; got != want {
		t.Fatalf("unexpected parameter type\nwant: %q\ngot:  %q", want, got)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "2\nz\n2\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestNumericLiteralsCastsAndPromotion(t *testing.T) {
	source := `let whole = 1
let frac = 1.5
let widened: float = whole
let narrowed: int = (int) frac
let sum: int = whole + 2
let mixed: float = whole + frac
let quotient: float = whole / 2
println(whole)
println(frac)
println(widened)
println(narrowed)
println(sum)
println(mixed)
println(quotient)
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
	if got, want := out.String(), "1\n1.5\n1\n1\n3\n2.5\n0.5\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestTypeCheckerRejectsFloatAssignedToInt(t *testing.T) {
	source := `let bad: int = 1.5`
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
		t.Fatalf("expected type checker to reject float assigned to int")
	}
}

func TestRuntimeTypeChecksDistinguishIntAndFloat(t *testing.T) {
	source := `def takeInt(value: int) -> string:
    return "int"
end

def takeFloat(value: float) -> string:
    return "float"
end

let whole: any = 1
let frac: any = 1.25
println(takeInt(whole))
println(takeFloat(frac))
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
	if got, want := out.String(), "int\nfloat\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFinalSupportsFunctionAndClassAliases(t *testing.T) {
	source := `class Box:
    value: number

    Box(v: number):
        this.value = v
    end
end

def add(a: number, b: number) -> number:
    return a + b
end

final Maker = Box
final op = add
let box = Maker(7)
println(box.value)
println(op(2, 5))
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
	if got, want := out.String(), "7\n7\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBasicEnumHelpers(t *testing.T) {
	source := `enum Color
    RED
    GREEN
    BLUE
end

let green = Color.valueOf("GREEN")
println(green.name)
println(green.ordinal)
println(Color.size())
let red = Color.valueOf("RED")
println(red.name)
println(green == Color.GREEN)
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
	if got, want := out.String(), "GREEN\n1\n3\nRED\ntrue\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestEnumConstructorAndMethod(t *testing.T) {
	source := `enum Planet
    MERCURY(3.7)
    EARTH(9.8)
    MARS(3.71)

    var gravity: number

    Planet(g: number):
        this.gravity = g
    end

    def weight(mass: number) -> number:
        return mass * this.gravity
    end
end

let earth = Planet.EARTH
println(earth.gravity)
println(earth.weight(75))
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
	if got, want := out.String(), "9.8\n735\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFinalEnumDeclarationWorks(t *testing.T) {
	source := `final enum Mode
    OFF
    ON
end

final active = Mode.ON
println(active.name)
println(Mode.valueOf("OFF").ordinal)
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
	if got, want := out.String(), "ON\n0\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCompoundAssignments(t *testing.T) {
	source := `let total = 10
total += 5
total -= 3
total *= 4
total /= 2
println(total)

class Counter:
    value: number

    Counter(start: number):
        this.value = start
    end
end

let counter = new Counter(8)
counter.value += 2
counter.value *= 3
println(counter.value)

let arr = [2, 4, 6]
arr[1] += 5
arr[2] *= 2
println(arr[1])
println(arr[2])

let table = {score: 7}
table["score"] += 4
println(table["score"])
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
	if got, want := out.String(), "24\n30\n9\n12\n11\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFastPathsForArraysMapsAndPureMethods(t *testing.T) {
	source := `class PairBox:
    left: number
    right: number

    PairBox(left: number, right: number):
        this.left = left
        this.right = right
    end

    def mix() -> number:
        return this.left * 3 + this.right * 5
    end
end

	let box = new PairBox(2, 3)
	let total: number = 0
	for i in range(0, 2):
    total += box.mix()
end

let arr = [10, 20, 30]
arr[1] += 5
println(arr[1])

let key = "score"
let table = {score: 7}
println(table[key])
table[key] += 4
println(table[key])
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
	disassembly := fn.Chunk.Disassemble(fn.Name)
	if !strings.Contains(disassembly, "GET_INDEX_ARRAY") {
		t.Fatalf("expected GET_INDEX_ARRAY in script chunk, got:\n%s", disassembly)
	}
	if !strings.Contains(disassembly, "SET_INDEX_ARRAY") {
		t.Fatalf("expected SET_INDEX_ARRAY in script chunk, got:\n%s", disassembly)
	}
	if !strings.Contains(disassembly, "GET_INDEX_MAP") {
		t.Fatalf("expected GET_INDEX_MAP in script chunk, got:\n%s", disassembly)
	}
	if !strings.Contains(disassembly, "SET_INDEX_MAP") {
		t.Fatalf("expected SET_INDEX_MAP in script chunk, got:\n%s", disassembly)
	}
	var pairBox *value.Class
	for _, constant := range fn.Chunk.Constants {
		if classValue, ok := constant.(*value.Class); ok && classValue.Name == "PairBox" {
			pairBox = classValue
			break
		}
	}
	if pairBox == nil {
		t.Fatalf("expected PairBox class constant")
	}
	slot, _, ok := pairBox.LookupMethodSlot("mix")
	if !ok {
		t.Fatalf("expected mix method slot")
	}
	if plan, ok := pairBox.LookupFastMethodBySlot(slot); !ok || plan == nil {
		t.Fatalf("expected fast method plan for mix")
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "25\n7\n11\n42\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestEndToEndClassExecution(t *testing.T) {
	source := `class Persona:
    nombre: string

    Persona(n: string):
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
    factor: number

    Worker(f: number):
        this.factor = f
    end

    def run(limit: number) -> number:
		let acc: number = 0
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
    factor: number

    Worker(f: number):
        this.factor = f
    end

    def run(limit: number) -> number:
		let acc: number = 0
        for i in range(0, limit):
            acc = acc + (i * this.factor)
        end
        return acc
    end
end

let worker = new Worker(3)
let total: number = 0
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
	if disassembly := fn.Chunk.Disassemble(fn.Name); !strings.Contains(disassembly, "ADD_NUM") && !strings.Contains(disassembly, "ADD_TO_LOCAL") {
		t.Fatalf("expected specialized numeric add in script chunk, got:\n%s", disassembly)
	}
}

func TestStaticFieldsAndMethods(t *testing.T) {
	source := `class MathHelper:
    static let PI: number = 3.14159
    static var count: number = 0

    static def square(x: number) -> number:
        return x * x
    end

    static def tick() -> number:
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

def mark() -> bool:
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

func TestArraysMapsAndIndexing(t *testing.T) {
	source := `let arr = [1, 2, 3]
println(arr[1])
arr[1] = 8
println(arr[1])

let sum = 0
for item in arr:
    sum = sum + item
end
println(sum)

let point = {x: 10, "y": 20}
println(point["x"])
point["x"] = 11
println(point["x"])
println(point["missing"])
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
	if got, want := out.String(), "2\n8\n12\n10\n11\nnil\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestStringIndexAndSlice(t *testing.T) {
	source := `let text = "abcde"
println(text[1])
println(text[1...3])
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
	if got, want := out.String(), "b\nbcd\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCharLiteralAndStringIndexYieldChar(t *testing.T) {
	source := `let initial: char = 'P'
let text = "olyloft"
let combined = initial + text
println(initial)
println(text[0])
println(combined)
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
	if got, want := out.String(), "P\no\nPolyloft\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCharStringComparisonAndCast(t *testing.T) {
	source := `let letter: char = 'a'
let text: String = (String) letter
println(letter == "a")
println("a" == letter)
println(letter < "b")
println(text)
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
	if got, want := out.String(), "true\ntrue\ntrue\na\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuiltinInterfaceMatrixForNativeTypes(t *testing.T) {
	source := `def dump(items: Iterable):
    let total = 0
    for item in items:
        total = total + item
    end
    println(total)
end

let seq: Iterable = range(0, 4)
dump(seq)

let slots: Indexable = [10, 20, 30]
println(slots[1])
slots[1] = 99
println(slots[1])

let text: Sliceable = "poly"
println(text[1...2])
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
	if got, want := out.String(), "6\n20\n99\nol\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuiltinInterfaceMatrixRejectsArrayAsIterable(t *testing.T) {
	source := `let bad: Iterable = [1, 2, 3]`
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
	err = sema.Check(program, registry)
	if err == nil {
		t.Fatalf("expected array to be rejected as Iterable")
	}
	if !strings.Contains(err.Error(), "cannot assign array to Iterable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestContainsOperatorSupportsNativeAndProtocolTypes(t *testing.T) {
	source := `class Bag implements Indexable<string, number>:
    var data: map<string, number>

    Bag(items: map<string, number>):
        this.data = items
    end

    def __get(key: string) -> number:
        return this.data[key]
    end

    def __set(key: string, value: number) -> void:
        this.data[key] = value
    end

    def __contains(key: string) -> bool:
        return key in this.data
    end
end

println(2 in [1, 2, 3])
println("ly" in "polyloft")
println("name" in {"name": 7})
let bag = Bag({"ok": 1})
println("ok" in bag)
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
	if got, want := out.String(), "true\ntrue\ntrue\ntrue\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestDeclaredGenericTypesBoundsAndWildcards(t *testing.T) {
	source := `class Box<T>:
    let value: T

    Box(value: T):
        this.value = value
    end

    def get() -> T:
        return this.value
    end
end

class Numbers implements Iterable<number>:
    var items: array<number>

    Numbers(items: array<number>):
        this.items = items
    end

	def __length() -> number:
        return len(this.items)
    end

	def __get(index: int) -> number:
        return this.items[index]
    end
end

def identity<T>(value: T) -> T:
    return value
end

def positive<T extends number>(value: T) -> T:
    return value
end

def sum(items: Iterable<? extends number>) -> number:
	let total: number = 0
    for item in items:
        total = total + item
    end
    return total
end

let box = Box(7)
println(box.get())
println(identity("ok"))
println(positive(9))
println(sum(Numbers([1, 2, 3])))
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
	if got, want := out.String(), "7\nok\n9\n6\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestGenericBoundsRejectInvalidArguments(t *testing.T) {
	source := `def onlyNumbers<T extends number>(value: T) -> T:
    return value
end

println(onlyNumbers("bad"))
`
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
	err = sema.Check(program, registry)
	if err == nil {
		t.Fatalf("expected bound violation")
	}
	if !strings.Contains(err.Error(), "does not satisfy bounds of T") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCustomProtocolMethodsDriveRuntimeSyntax(t *testing.T) {
	source := `class Buffer implements Iterable, Unstructured, Indexable, Sliceable:
    var data: array

    Buffer(items: array):
        this.data = items
    end

	def __length() -> number:
        return len(this.data)
    end

	def __get(index: int) -> any:
        return this.data[index]
    end

	def __set(index: int, value: any) -> void:
        this.data[index] = value
    end

    def __contains(value: any) -> bool:
        for item in this.data:
            if item == value:
                return true
            end
        end
        return false
    end

	def __slice(start: int, finish: int) -> Buffer:
        let cut = Buffer([])
        cut.data = this.data[start...finish]
        return cut
    end

	def __pieces() -> number:
        return 2
    end

	def __get_piece(index: int) -> any:
        if index == 0:
            return this.data[0]
        end
        return this.data[1]
    end
end

let buf = Buffer([3, 4, 5])
let total = 0
for item in buf:
    total = total + item
end
println(total)

buf[1] = 9
println(buf[1])

let left, right = buf
println(left + right)

let tail = buf[1...2]
println(tail[0])
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
	if got, want := out.String(), "12\n9\n12\n9\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestCollectionInterfaceIsAvailableToUserClasses(t *testing.T) {
	source := `class Bag implements Collection:
	var count: number
	var first: any
	var second: any

    Bag():
		this.count = 0
		this.first = nil
		this.second = nil
    end

    def size() -> number:
		return this.count
    end

    def isEmpty() -> bool:
		return this.count == 0
    end

    def add(element: any) -> void:
		if this.count == 0:
			this.first = element
		else:
			this.second = element
		end
		this.count = this.count + 1
    end

    def remove(element: any) -> bool:
		if this.count > 0 && this.first == element:
			this.first = this.second
			this.second = nil
			this.count = this.count - 1
			return true
        end
		if this.count > 1 && this.second == element:
			this.second = nil
			this.count = this.count - 1
			return true
		end
		return false
    end

    def contains(element: any) -> bool:
		if this.count > 0 && this.first == element:
			return true
		end
		if this.count > 1 && this.second == element:
			return true
        end
        return false
    end

    def clear() -> void:
		this.count = 0
		this.first = nil
		this.second = nil
    end

    def asArray() -> array:
		if this.count == 0:
			return []
		end
		if this.count == 1:
			return [this.first]
		end
		return [this.first, this.second]
    end
end

let bag: Collection = Bag()
println(bag.isEmpty())
bag.add(7)
bag.add(9)
println(bag.size())
println(bag.contains(9))
println(bag.asArray()[0])
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
	if got, want := out.String(), "true\n2\ntrue\n7\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestGenericCollectionInterfaceSpecializesMembers(t *testing.T) {
	source := `class NumberBag implements Collection<number>:
	var first: number
	var second: number
	var count: number

	NumberBag(first: number):
		this.first = first
		this.second = 0
		this.count = 1
	end

	def size() -> number:
		return this.count
	end

	def isEmpty() -> bool:
		return this.count == 0
	end

	def add(element: number) -> void:
		if this.count == 0:
			this.first = element
		else:
			this.second = element
		end
		this.count = this.count + 1
	end

	def remove(element: number) -> bool:
		return false
	end

	def contains(element: number) -> bool:
		if this.count > 0 && this.first == element:
			return true
		end
		if this.count > 1 && this.second == element:
			return true
		end
		return false
	end

	def clear() -> void:
		return
	end

	def asArray() -> array<number>:
		if this.count == 1:
			return [this.first]
		end
		return [this.first, this.second]
	end
end

let bag: Collection<number> = NumberBag(4)
println(bag.isEmpty())
bag.add(8)
println(bag.size())
println(bag.contains(8))
let values: array<number> = bag.asArray()
println(values[1])
println(bag.remove(4))
println(bag.size())
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
	if got, want := out.String(), "false\n2\ntrue\n8\nfalse\n2\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestArrayAndTupleSlice(t *testing.T) {
	source := `let arr = [1, 2, 3, 4]
let piece = arr[1...2]
println(piece[0])
println(piece[1])

let tup = (10, 20, 30, 40)
let piece2 = tup[1...3]
println(piece2[0])
println(piece2[2])
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
	if got, want := out.String(), "2\n3\n20\n40\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestForDestructureArrayPairs(t *testing.T) {
	source := `let total = 0
for a, b in [(1, 2), (3, 4)]:
    total = total + a + b
end
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
	if got, want := out.String(), "10\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestForMapKeysAndDestructurePairs(t *testing.T) {
	source := `let data = {"a": 2, "c": 4}
let keys = ""
for key in data:
    keys = keys + key
end
println(keys)

let total = 0
for key, value in data:
    println(key)
    total = total + value
end
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
	if got, want := out.String(), "ac\na\nc\n6\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestSwitchValueAndTypeCases(t *testing.T) {
	source := `let code = 3
switch code:
    case 1:
        println("one")
    case 2:
    case 3:
        println("small")
    default:
        println("other")
end

def classify(value):
    switch value:
        case (n: number):
            return "n=" + n
        case (s: string):
            return "s=" + s
        default:
            return "other"
    end
end

println(classify(7))
println(classify("ok"))

def grade(score):
    switch true:
        case score >= 90:
            return "A"
        case score >= 80:
            return "B"
        default:
            return "C"
    end
end

println(grade(85))
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
	var classifyFn *bytecode.Function
	for _, constant := range fn.Chunk.Constants {
		if nested, ok := constant.(*bytecode.Function); ok && nested.Name == "classify" {
			classifyFn = nested
			break
		}
	}
	if classifyFn == nil {
		t.Fatalf("expected nested function classify in constants")
	}
	if disassembly := classifyFn.Chunk.Disassemble(classifyFn.Name); !strings.Contains(disassembly, "MATCH_TYPE") {
		t.Fatalf("expected MATCH_TYPE in classify lowering, got:\n%s", disassembly)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "small\nn=7\ns=ok\nB\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestAccessModifiersEnforcedAndProtectedWorksInSubclass(t *testing.T) {
	source := `class Base:
    protected let value: number = 4
    private let hidden: number = 9

    def reveal() -> number:
        return this.hidden
    end
end

class Child extends Base:
    def total() -> number:
        return this.value + this.reveal()
    end
end

println(new Child().total())
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
	if got, want := out.String(), "13\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestTypeCheckerRejectsPrivateMemberAccessOutsideClass(t *testing.T) {
	source := `class Vault:
    private let code: number = 123
end

let vault = new Vault()
println(vault.code)
`
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
		t.Fatalf("expected private member access to be rejected")
	}
}

func TestAccessModifierAliasesWork(t *testing.T) {
	source := `class Vault:
    prot let value: number = 7
    priv let hidden: number = 5

    pub def reveal() -> number:
        return this.hidden
    end
end

class Child extends Vault:
    pub def total() -> number:
        return this.value + this.reveal()
    end
end

println(new Child().total())
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
	if got, want := out.String(), "12\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestParserRejectsLegacySuperclassAngleSyntax(t *testing.T) {
	source := `class Child < Base:
end
`
	tokens, err := lexer.Scan(source)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if _, err := parser.Parse(tokens); err == nil {
		t.Fatalf("expected parser to reject legacy superclass syntax with <")
	}
}

func TestTypeCheckerRejectsPrivateConstructorOutsideClass(t *testing.T) {
	source := `class Secret:
    private Secret():
    end

    static def build() -> Secret:
        return new Secret()
    end
end

let secret = new Secret()
println(secret)
`
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
		t.Fatalf("expected private constructor access to be rejected")
	}
}

func TestProtectedConstructorWorksOnlyInSubclass(t *testing.T) {
	source := `class Base:
	    value: number

    protected Base(value: number):
        this.value = value
    end

	    def reveal() -> number:
	        return this.value
	    end
end

class Child extends Base:
	    static def emit(value: number) -> number:
	        return new Base(value).reveal()
    end
end

println(Child.emit(9))
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
	if got, want := out.String(), "9\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestDeclarativeInterfacesAndLambdaFunctionalDispatch(t *testing.T) {
	source := `interface Named:
    getName() -> string
end

interface Mapper:
    apply(x: number) -> number
end

class Person implements Named:
    let name: string

    Person(name: string):
        this.name = name
    end

    def getName() -> string:
        return this.name
    end
end

def show(value: Named):
    println(value.getName())
end

def useMapper(mapper: Mapper, input: number) -> number:
    return mapper.apply(input)
end

show(new Person("Ana"))
println(useMapper((x: number) => x * 3, 7))
let mapper: Mapper = (x: number) => x + 5
println(mapper.apply(10))
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
	if got, want := out.String(), "Ana\n21\n15\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReferenceCastSupportsInterfaces(t *testing.T) {
	source := `interface Named:
    getName() -> string
end

class Person implements Named:
    let name: string

    Person(name: string):
        this.name = name
    end

    def getName() -> string:
        return this.name
    end
end

let value: any = new Person("Ana")
let named = (Named) value
println(named.getName())
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
	if disassembly := fn.Chunk.Disassemble(fn.Name); !strings.Contains(disassembly, "CAST_REF") || !strings.Contains(disassembly, "(Named)") {
		t.Fatalf("expected CAST_REF Named in script chunk, got:\n%s", disassembly)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "Ana\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReferenceCastRejectsWrongRuntimeValue(t *testing.T) {
	source := `interface Named:
    getName() -> string
end

let value: any = "Ana"
let named = (Named) value
println(named)
`

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
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&bytes.Buffer{}, registry)
	if _, err := machine.Run(fn); err == nil || !strings.Contains(err.Error(), "cannot cast String to Named") {
		t.Fatalf("expected runtime cast failure, got: %v", err)
	}
}

func TestLambdaBlockImplicitReturnAndClosureCapture(t *testing.T) {
	source := `def makeCounter(start: number):
    let current = start
    return () =>:
        current = current + 1
        current
    end
end

let counter = makeCounter(10)
println(counter())
println(counter())

let scale = 4
let calc = (x: number) =>:
    let doubled = x * 2
    doubled + scale
end
println(calc(3))
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
	if got, want := out.String(), "11\n12\n10\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
	if disassembly := fn.Chunk.Disassemble(fn.Name); !strings.Contains(disassembly, "CLOSURE") {
		t.Fatalf("expected CLOSURE in script chunk, got:\n%s", disassembly)
	}
}

func TestSAMWrapperUsesExplicitWrapping(t *testing.T) {
	source := `interface Mapper:
    apply(x: number) -> number
end

def execute(mapper: Mapper, value: number) -> number:
    return mapper.apply(value)
end

let mapper: Mapper = (x: number) =>:
    let next = x + 2
    next * 5
end

println(execute(mapper, 4))
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
	if disassembly := fn.Chunk.Disassemble(fn.Name); !strings.Contains(disassembly, "WRAP_INTERFACE") {
		t.Fatalf("expected WRAP_INTERFACE in script chunk, got:\n%s", disassembly)
	}
	machine := vm.NewWithRegistry(&out, registry)
	if _, err := machine.Run(fn); err != nil {
		t.Fatalf("vm run failed: %v", err)
	}
	if got, want := out.String(), "30\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestImportsSupportPublicProtectedAndImportedTypes(t *testing.T) {
	tempDir := t.TempDir()
	moduleSource := `public class Greeter:
    let prefix: string

    Greeter(prefix: string):
        this.prefix = prefix
    end

    def say(name: string) -> string:
        return this.prefix + name
    end
end

protected def helper(x: number) -> number:
    return x + 1
end

public def twice(x: number) -> number:
    return x * 2
end

public const ANSWER = 42

public interface Named:
    getName() -> string
end

let secret = 99
`
	mainSource := `import util { Greeter, helper, twice, ANSWER, Named }

class Person implements Named:
    let name: string

    Person(name: string):
        this.name = name
    end

    def getName() -> string:
        return this.name
    end
end

def show(value: Named):
    println(value.getName())
end

let greeter = Greeter("Hi ")
println(greeter.say("Ana"))
println(twice(3))
println(helper(4))
println(ANSWER)
show(Person("Bea"))
`
	if err := os.WriteFile(filepath.Join(tempDir, "util.pf"), []byte(moduleSource), 0o644); err != nil {
		t.Fatalf("write util failed: %v", err)
	}
	mainPath := filepath.Join(tempDir, "main.pf")
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "Hi Ana\n6\n5\n42\nBea\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestImportsRejectPrivateSymbolAcrossFiles(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "util.pf"), []byte("let secret = 99\n"), 0o644); err != nil {
		t.Fatalf("write util failed: %v", err)
	}
	mainPath := filepath.Join(tempDir, "main.pf")
	if err := os.WriteFile(mainPath, []byte("import util { secret }\nprintln(secret)\n"), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	if _, err := runPreparedFile(t, mainPath); err == nil {
		t.Fatalf("expected private import to fail")
	}
}

func TestImportsRejectProtectedSymbolOutsideFolder(t *testing.T) {
	rootDir := t.TempDir()
	moduleDir := filepath.Join(rootDir, "pkg")
	otherDir := filepath.Join(rootDir, "app")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module failed: %v", err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir app failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "util.pf"), []byte("protected def helper(x: number) -> number:\n    return x + 1\nend\n"), 0o644); err != nil {
		t.Fatalf("write util failed: %v", err)
	}
	mainPath := filepath.Join(otherDir, "main.pf")
	if err := os.WriteFile(mainPath, []byte("import pkg.util { helper }\nprintln(helper(1))\n"), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	if _, err := runPreparedFile(t, mainPath); err == nil {
		t.Fatalf("expected protected import outside folder to fail")
	}
}

func TestNamespaceImportBuildsNestedModules(t *testing.T) {
	tempDir := t.TempDir()
	libDir := filepath.Join(tempDir, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "math.pf"), []byte("public def twice(x: number) -> number:\n    return x * 2\nend\n"), 0o644); err != nil {
		t.Fatalf("write math failed: %v", err)
	}
	mainPath := filepath.Join(tempDir, "main.pf")
	if err := os.WriteFile(mainPath, []byte("import lib.math\nprintln(lib.math.twice(5))\n"), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "10\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestEmbeddedStdlibImportLoadsPolyloftCommon(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Integer, Float, Double, String, Boolean, Char, CharArray, Bytes }\n" +
		"let count = Integer(7)\n" +
		"let count2 = Integer(10)\n" +
		"let ratio = Float(2.5)\n" +
		"let precise = Double(4.5)\n" +
		"let name = String(\"Polyloft\")\n" +
		"let enabled = Boolean(true)\n" +
		"let disabled = Boolean(false)\n" +
		"let letter = Char('P')\n" +
		"let charData: array<char> = ['P', 'L']\n" +
		"let chars = CharArray(charData)\n" +
		"let packet = Bytes([1, 2, 3])\n" +
		"switch count:\n" +
		"    case (n: Integer):\n" +
		"        println(true)\n" +
		"    default:\n" +
		"        println(false)\n" +
		"end\n" +
		"switch count:\n" +
		"    case (n: number):\n" +
		"        println(true)\n" +
		"    default:\n" +
		"        println(false)\n" +
		"end\n" +
		"switch 10:\n" +
		"    case (n: Integer):\n" +
		"        println(true)\n" +
		"    default:\n" +
		"        println(false)\n" +
		"end\n" +
		"switch 10:\n" +
		"    case (n: number):\n" +
		"        println(true)\n" +
		"    default:\n" +
		"        println(false)\n" +
		"end\n" +
		"println(count.unwrap() + 5)\n" +
		"println(count2.unwrap() * 2)\n" +
		"println(ratio.unwrap() + precise.unwrap())\n" +
		"println(Integer(9).compareTo(3) > 0)\n" +
		"if enabled:\n" +
		"    println(\"enabled\")\n" +
		"else:\n" +
		"    println(\"disabled\")\n" +
		"end\n" +
		"if disabled:\n" +
		"    println(\"wrong\")\n" +
		"else:\n" +
		"    println(\"guarded\")\n" +
		"end\n" +
		"println(!disabled)\n" +
		"println(enabled && true)\n" +
		"println(disabled || true)\n" +
		"println(enabled == true)\n" +
		"println(count.value)\n" +
		"println(count2.value)\n" +
		"println(name.value)\n" +
		"println(letter.value)\n" +
		"println(chars.data[1])\n" +
		"println(packet.data[1])\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\ntrue\ntrue\ntrue\n12\n20\n7\ntrue\nenabled\nguarded\ntrue\ntrue\ntrue\ntrue\n7\n10\nPolyloft\nP\nL\n2\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestEmbeddedStdlibImportLoadsPolyloftCollections(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.collections { List, Deque, Set, HashSet }\n" +
		"def acceptsList(items: List) -> bool:\n" +
		"    return true\n" +
		"end\n" +
		"def acceptsDeque(items: Deque) -> bool:\n" +
		"    return true\n" +
		"end\n" +
		"let tags: Set = HashSet.from([\"go\", \"vm\", \"go\"])\n" +
		"let concrete = HashSet.from([\"go\", \"vm\", \"go\"])\n" +
		"println(tags.contains(\"go\"))\n" +
		"println(\"vm\" in concrete.asArray())\n" +
		"println(concrete.size())\n" +
		"println(concrete.remove(\"vm\"))\n" +
		"println(concrete.size())\n" +
		"println(concrete.contains(\"go\"))\n" +
		"println(true)\n" +
		"println(true)\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\ntrue\n2\ntrue\n1\ntrue\ntrue\ntrue\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestDumpProducesReadablePfbc(t *testing.T) {
	src := `
let x = 1 + 2
println(x)
`
	tokens, err := lexer.Scan(src)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &bytes.Buffer{})
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// dump to temp file
	fpath := filepath.Join(t.TempDir(), "out.pfbc")
	f, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("create pfbc: %v", err)
	}
	if _, err := fn.WriteTo(f); err != nil {
		t.Fatalf("write pfbc: %v", err)
	}
	f.Close()
	// read back
	rf, err := os.Open(fpath)
	if err != nil {
		t.Fatalf("open pfbc: %v", err)
	}
	fn2, err := bytecode.ReadFunction(rf)
	rf.Close()
	if err != nil {
		t.Fatalf("read pfbc: %v", err)
	}
	// simple sanity: disassembly should match
	if fn2.Chunk.Disassemble(fn2.Name) != fn.Chunk.Disassemble(fn.Name) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestEmbeddedStdlibImportLoadsPolyloftFunction(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.function { Predicate, Function, Consumer, Supplier, UnaryOperator }\n" +
		"def emit(sink: Consumer<? super string>, value: string) -> void:\n" +
		"    sink.accept(value)\n" +
		"end\n" +
		"let starts: Predicate<string> = (value: string) => value[0] == \"p\"\n" +
		"let maker: Supplier<string> = () => \"made\"\n" +
		"let lengthOf: Function<string, number> = (value: string) => len(value)\n" +
		"let doubler: UnaryOperator<number> = (value: number) => value * 2\n" +
		"println(starts.test(\"poly\"))\n" +
		"println(maker.get())\n" +
		"println(lengthOf.apply(\"abcd\"))\n" +
		"println(doubler.apply(9))\n" +
		"emit((value: string) => println(value), \"sink\")\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\nmade\n4\n18\nsink\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestFunctionalInterfacesUseContextualLambdaTyping(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.function { Predicate, Function, Consumer, UnaryOperator }\n" +
		"def acceptPredicate(test: Predicate<string>, value: string) -> bool:\n" +
		"    return test.test(value)\n" +
		"end\n" +
		"def transform(mapper: Function<string, number>, value: string) -> number:\n" +
		"    return mapper.apply(value)\n" +
		"end\n" +
		"def writeTo(sink: Consumer<? super string>, value: string) -> void:\n" +
		"    sink.accept(value)\n" +
		"end\n" +
		"let starts: Predicate<string> = (value) => value[0] == \"p\"\n" +
		"let lengthOf: Function<string, number> = (value) => len(value)\n" +
		"let doubler: UnaryOperator<number> = (value) => value * 2\n" +
		"let printer: Consumer<? super string> = (value) =>:\n" +
		"    println(value)\n" +
		"end\n" +
		"println(starts.test(\"poly\"))\n" +
		"println(lengthOf.apply(\"abcd\"))\n" +
		"println(doubler.apply(9))\n" +
		"println(acceptPredicate((value) => value[1] == \"o\", \"go\"))\n" +
		"println(transform((value) => len(value), \"hello\"))\n" +
		"writeTo(printer, \"sink\")\n" +
		"writeTo((value) => println(value), \"inline\")\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\n4\n18\ntrue\n5\nsink\ninline\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestEmbeddedStdlibConcurrentAsyncAcceptsSupplierLambda(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.concurrent { async }\n" +
		"let future = async(() => 5 + 7)\n" +
		"println(future.get())\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "12\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestAsyncClosureCapturesGlobalVariable(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	// Regression: an async closure referencing a slot-resolved global (a
	// top-level `let`) used to panic with an out-of-range global-slot read,
	// because CallClosureIsolated built the isolated VM without the
	// slot-indexed globals (globalSlots/globalDefined).
	mainSource := "import polyloft.concurrent { async }\n" +
		"let base = 100\n" +
		"let f = async(() => base + 23)\n" +
		"println(f.get())\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "123\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestOmittedGenericArgsDefaultToAnyAndConcatWithString(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.collections { ArrayList }\n" +
		"import polyloft.maps { HashMap }\n" +
		"def stringify<T>(value: T) -> string:\n" +
		"    return value + \"!\"\n" +
		"end\n" +
		"let items: ArrayList = ArrayList()\n" +
		"items.add(\"poly\")\n" +
		"items.add(7)\n" +
		"println(items.get(0))\n" +
		"println(stringify(items.get(1)))\n" +
		"let store: HashMap = HashMap()\n" +
		"store.put(\"answer\", 42)\n" +
		"store.put(\"label\", \"ok\")\n" +
		"println(store.get(\"answer\") + \"!\")\n" +
		"println(\"value=\" + store.get(\"label\"))\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "poly\n7!\n42!\nvalue=ok\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestEmbeddedStdlibWrappersAndMapsExposeObjectApis(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Integer, String, Boolean, CharArray, Bytes }\n" +
		"import polyloft.maps { Map, HashMap, SetMap }\n" +
		"let count = Integer(7)\n" +
		"let negative = Integer(-5)\n" +
		"println(count.intValue())\n" +
		"println(negative.abs().negate().intValue())\n" +
		"println(count.compareTo(9))\n" +
		"println(count.compareTo(7))\n" +
		"println(count.toString())\n" +
		"let text = String(\"Polyloft\")\n" +
		"println(text.length())\n" +
		"println(text.isEmpty())\n" +
		"println(text.charAt(2))\n" +
		"println(text.substring(1, 4).concat(\"!\").toString())\n" +
		"println(text.concat(\" VM\").toString())\n" +
		"println(text.startsWith(\"Poly\"))\n" +
		"println(text.endsWith(\"loft\"))\n" +
		"println(text.contains(\"ylo\"))\n" +
		"println(text.indexOf(\"lo\"))\n" +
		"println(String(\"ha\").repeat(3).toString())\n" +
		"let enabled = Boolean(true)\n" +
		"println(enabled.negate().booleanValue())\n" +
		"println(enabled.and(false).booleanValue())\n" +
		"println(enabled.or(false).booleanValue())\n" +
		"println(enabled.isTrue())\n" +
		"println(enabled.negate().isFalse())\n" +
		"let charsData: array<char> = ['A', 'B', 'C']\n" +
		"let chars = CharArray(charsData)\n" +
		"println(chars.length())\n" +
		"println(chars.get(1))\n" +
		"println(chars.contains('B'))\n" +
		"println(chars.indexOf('C'))\n" +
		"println(chars.toString())\n" +
		"let packet = Bytes([1, 2, 3])\n" +
		"println(packet.length())\n" +
		"println(packet.get(2))\n" +
		"println(packet.size())\n" +
		"println(packet.contains(2))\n" +
		"println(packet.indexOf(3))\n" +
		"println(packet.asArray()[1])\n" +
		"let view = Map.from({\"a\": 1, \"b\": 2})\n" +
		"println(view.size())\n" +
		"println(view.containsKey(\"b\"))\n" +
		"println(view.getOrDefault(\"missing\", 9))\n" +
		"println(view.delete(\"a\"))\n" +
		"println(view.size())\n" +
		"println(view.keys()[0])\n" +
		"println(view.values()[0])\n" +
		"let store = HashMap.from({\"name\": \"ana\"}).put(7, \"lucky\")\n" +
		"store.put(true, \"on\")\n" +
		"println(store.size())\n" +
		"println(store.get(\"name\"))\n" +
		"println(store.get(7))\n" +
		"println(store.containsKey(true))\n" +
		"println(store.delete(7))\n" +
		"println(store.size())\n" +
		"println(store.containsKey(\"name\"))\n" +
		"store.clear()\n" +
		"println(store.isEmpty())\n" +
		"let tags = SetMap.from([\"go\", \"vm\"]).add(7)\n" +
		"println(tags.contains(\"go\"))\n" +
		"println(tags.contains(7))\n" +
		"println(tags.size())\n" +
		"println(tags.delete(\"vm\"))\n" +
		"println(tags.size())\n" +
		"tags.clear()\n" +
		"println(tags.isEmpty())\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "7\n-5\n-1\n0\n7\n8\nfalse\nl\nolyl!\nPolyloft VM\ntrue\ntrue\ntrue\n4\nhahaha\nfalse\nfalse\ntrue\ntrue\ntrue\n3\nB\ntrue\n2\nABC\n3\n3\n3\ntrue\n2\n2\n2\ntrue\n9\ntrue\n1\nb\n2\n3\nana\nlucky\ntrue\ntrue\n2\ntrue\ntrue\ntrue\ntrue\n3\ntrue\n2\ntrue\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestRecordGeneratesConstructorAndImmutableFields(t *testing.T) {
	source := `record Point(x: number, y: number)
    def sum() -> number:
        return this.x + this.y
    end
end

let point = Point(2, 5)
println(point.x)
println(point.sum())
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
	if got, want := out.String(), "2\n7\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRecordRejectsMutation(t *testing.T) {
	source := `record Point(x: number, y: number)
end

let point = Point(2, 5)
point.x = 9
`

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
	if err := sema.Check(program, registry); err != nil {
		t.Fatalf("type check failed: %v", err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	machine := vm.NewWithRegistry(&bytes.Buffer{}, registry)
	if _, err := machine.Run(fn); err == nil {
		t.Fatalf("expected record field mutation to fail")
	}
}

func TestAbstractClassRequiresOverrideAndRejectsInstantiation(t *testing.T) {
	missingOverride := `abstract class Shape:
    abstract def area() -> number
end

class Square extends Shape:
end
`

	tokens, err := lexer.Scan(missingOverride)
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
		t.Fatalf("expected concrete subclass without override to fail")
	}

	instantiation := `abstract class Shape:
    abstract def area() -> number
end

let shape = new Shape()
`
	tokens, err = lexer.Scan(instantiation)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err = parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	registry = bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &bytes.Buffer{})
	if err := sema.Check(program, registry); err == nil {
		t.Fatalf("expected abstract class instantiation to fail")
	}
}

func TestAbstractClassAllowsConcreteSubclass(t *testing.T) {
	source := `abstract class Shape:
    abstract def area() -> number
end

class Square extends Shape:
    let size: number

    Square(size: number):
        this.size = size
    end

    def area() -> number:
        return this.size * this.size
    end
end

println(Square(4).area())
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
	if got, want := out.String(), "16\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestSealedClassRestrictsSubclasses(t *testing.T) {
	valid := `sealed class Expr(Add):
end

class Add extends Expr:
end

println(Add())
`

	tokens, err := lexer.Scan(valid)
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

	invalid := `sealed class Expr(Add):
end

class Mul extends Expr:
end
`
	tokens, err = lexer.Scan(invalid)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	program, err = parser.Parse(tokens)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	registry = bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, &bytes.Buffer{})
	if err := sema.Check(program, registry); err == nil {
		t.Fatalf("expected non-permitted sealed subclass to fail")
	}
}

func TestInstanceFieldInitializersRunOnObjectCreation(t *testing.T) {
	source := `
def makeLabel() -> String:
    return "ready"
end

def makeSeed() -> int:
    return 41
end

class LazyLabel:
    var label: String = makeLabel()
end

class Counter:
    var seed: int = makeSeed()

    Counter():
        this.seed = this.seed + 1
    end
end

println(LazyLabel().label)
println(Counter().seed)
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
	if got, want := out.String(), "ready\n42\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestPolyloftTomlLibRootResolution(t *testing.T) {
	rootDir := t.TempDir()
	libDir := filepath.Join(rootDir, "lib", "math")
	srcDir := filepath.Join(rootDir, "src", "app")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib failed: %v", err)
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "polyloft.toml"), []byte("name = \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write polyloft.toml failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "util.pf"), []byte("public def twice(x: number) -> number:\n    return x * 2\nend\n"), 0o644); err != nil {
		t.Fatalf("write util failed: %v", err)
	}
	mainPath := filepath.Join(srcDir, "main.pf")
	if err := os.WriteFile(mainPath, []byte("import math.util { twice }\nprintln(twice(5))\n"), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "10\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestImportedAbstractClassRestrictionsCrossModules(t *testing.T) {
	tempDir := t.TempDir()
	baseSource := `public abstract class Shape:
    abstract def area() -> number
end
`
	if err := os.WriteFile(filepath.Join(tempDir, "shape.pf"), []byte(baseSource), 0o644); err != nil {
		t.Fatalf("write shape failed: %v", err)
	}

	invalidMain := `import shape { Shape }

class Square extends Shape:
end
`
	invalidMainPath := filepath.Join(tempDir, "invalid_main.pf")
	if err := os.WriteFile(invalidMainPath, []byte(invalidMain), 0o644); err != nil {
		t.Fatalf("write invalid main failed: %v", err)
	}
	if _, err := runPreparedFile(t, invalidMainPath); err == nil {
		t.Fatalf("expected imported abstract superclass requirement to fail")
	}

	instantiateMain := `import shape { Shape }
let shape = new Shape()
`
	instantiateMainPath := filepath.Join(tempDir, "instantiate_main.pf")
	if err := os.WriteFile(instantiateMainPath, []byte(instantiateMain), 0o644); err != nil {
		t.Fatalf("write instantiate main failed: %v", err)
	}
	if _, err := runPreparedFile(t, instantiateMainPath); err == nil {
		t.Fatalf("expected imported abstract class instantiation to fail")
	}

	validMain := `import shape { Shape }

class Square extends Shape:
    let size: number

    Square(size: number):
        this.size = size
    end

    def area() -> number:
        return this.size * this.size
    end
end

println(Square(6).area())
`
	validMainPath := filepath.Join(tempDir, "valid_main.pf")
	if err := os.WriteFile(validMainPath, []byte(validMain), 0o644); err != nil {
		t.Fatalf("write valid main failed: %v", err)
	}
	output, err := runPreparedFile(t, validMainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "36\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestImportedSealedRestrictionsCrossModules(t *testing.T) {
	tempDir := t.TempDir()
	sealedClassSource := `public sealed class Expr(Add):
end

public class Add extends Expr:
end
`
	if err := os.WriteFile(filepath.Join(tempDir, "expr.pf"), []byte(sealedClassSource), 0o644); err != nil {
		t.Fatalf("write expr failed: %v", err)
	}
	invalidSubclassMain := `import expr { Expr }

class Mul extends Expr:
end
`
	invalidSubclassPath := filepath.Join(tempDir, "invalid_subclass_main.pf")
	if err := os.WriteFile(invalidSubclassPath, []byte(invalidSubclassMain), 0o644); err != nil {
		t.Fatalf("write invalid subclass main failed: %v", err)
	}
	if _, err := runPreparedFile(t, invalidSubclassPath); err == nil {
		t.Fatalf("expected imported sealed superclass restriction to fail")
	}

	sealedInterfaceSource := `public sealed interface Named(Person):
    getName() -> string
end
`
	if err := os.WriteFile(filepath.Join(tempDir, "named.pf"), []byte(sealedInterfaceSource), 0o644); err != nil {
		t.Fatalf("write named failed: %v", err)
	}
	invalidImplMain := `import named { Named }

class Animal implements Named:
    def getName() -> string:
        return "Animal"
    end
end
`
	invalidImplPath := filepath.Join(tempDir, "invalid_impl_main.pf")
	if err := os.WriteFile(invalidImplPath, []byte(invalidImplMain), 0o644); err != nil {
		t.Fatalf("write invalid impl main failed: %v", err)
	}
	if _, err := runPreparedFile(t, invalidImplPath); err == nil {
		t.Fatalf("expected imported sealed interface restriction to fail")
	}
}

func TestTypeCheckerRejectsWrongGenericElementType(t *testing.T) {
	// Creamos una clase gen+�rica simple para demostrar la especializaci+�n.
	source := `
interface Container<T>:
    add(item: T) -> void
end

class IntContainer implements Container<int>:
    def add(item: int) -> void:
        // no-op
    end
end

let c: Container<int> = IntContainer()
c.add("hola")
`
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
		t.Fatalf("expected type checker to reject wrong element type in generic list")
	}
}

func TestFixedArrayHasNoAppend(t *testing.T) {
	// Los arrays son de tama+�o fijo; no existe append.
	source := `let xs: array<int> = []
	xs.append(1)
`
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
	err = sema.Check(program, registry)
	if err == nil || !strings.Contains(err.Error(), "append") {
		t.Fatalf("expected error about missing append on fixed array, got: %v", err)
	}
}

func TestEmbeddedStdlibBytesContractExtended(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Bytes }\n" +
		"let fromHex = Bytes.fromHex(\"0x48656C6C6F\")\n" +
		"let suffix = Bytes([33])\n" +
		"let merged = fromHex.concat(suffix)\n" +
		"let expected = Bytes([72, 101, 108, 108, 111])\n" +
		"println(fromHex.asHex())\n" +
		"println(fromHex.length())\n" +
		"println(fromHex[1])\n" +
		"println(merged.asHex())\n" +
		"println(fromHex.slice(2).asHex())\n" +
		"println(fromHex[1...3].asHex())\n" +
		"println(fromHex.contains(108))\n" +
		"println(fromHex.indexOf(111))\n" +
		"println(fromHex.equals(expected))\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "48656c6c6f\n5\n101\n48656c6c6f21\n6c6c6f\n656c6c\ntrue\n4\ntrue\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestEmbeddedStdlibBytesRejectInvalidInputs(t *testing.T) {
	tempDir := t.TempDir()
	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name:    "odd hex digits",
			source:  "import polyloft.common { Bytes }\nprintln(Bytes.fromHex(\"abc\"))\n",
			message: "even number of hexadecimal digits",
		},
		{
			name:    "invalid hex digit",
			source:  "import polyloft.common { Bytes }\nprintln(Bytes.fromHex(\"zz\"))\n",
			message: "hexadecimal digits",
		},
		{
			name:    "byte range",
			source:  "import polyloft.common { Bytes }\nprintln(Bytes([300]))\n",
			message: "between 0 and 255",
		},
		{
			name:    "slice bounds",
			source:  "import polyloft.common { Bytes }\nprintln(Bytes([1, 2]).slice(3))\n",
			message: "slice index out of range",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			mainPath := filepath.Join(tempDir, strings.ReplaceAll(testCase.name, " ", "_")+".pf")
			if err := os.WriteFile(mainPath, []byte(testCase.source), 0o644); err != nil {
				t.Fatalf("write main failed: %v", err)
			}
			_, err := runPreparedFile(t, mainPath)
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("expected error containing %q, got: %v", testCase.message, err)
			}
		})
	}
}

func TestEmbeddedStdlibCompositeEnumRecordCollectionWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.collections { ArrayList }\n" +
		"import polyloft.function { Function, Predicate }\n" +
		"import polyloft.maps { HashMap }\n" +
		"import polyloft.common { Pair }\n" +
		"sealed enum Stage\n" +
		"    READY(1)\n" +
		"    RUNNING(2)\n" +
		"    DONE(3)\n" +
		"    let weight: number\n" +
		"    Stage(weight: number):\n" +
		"        this.weight = weight\n" +
		"    end\n" +
		"    def decorate(name: string) -> string:\n" +
		"        return name + \":\" + this.weight\n" +
		"    end\n" +
		"end\n" +
		"\n" +
		"record Job(name: string, stage: Stage)\n" +
		"    def label() -> string:\n" +
		"        return this.name + \"@\" + this.stage.name\n" +
		"    end\n" +
		"end\n" +
		"\n" +
		"def project(items: array<Job>, mapper: Function<Job, string>) -> ArrayList<string>:\n" +
		"    let out = ArrayList<string>()\n" +
		"    for item in items:\n" +
		"        out.add(mapper.apply(item))\n" +
		"    end\n" +
		"    return out\n" +
		"end\n" +
		"\n" +
		"def countMatches(items: array<Job>, test: Predicate<Job>) -> int:\n" +
		"    let total = 0\n" +
		"    for item in items:\n" +
		"        if test.test(item):\n" +
		"            total += 1\n" +
		"        end\n" +
		"    end\n" +
		"    return total\n" +
		"end\n" +
		"\n" +
		"let jobs: array<Job> = [Job(\"build\", Stage.READY), Job(\"ship\", Stage.DONE), Job(\"test\", Stage.RUNNING)]\n" +
		"let labels = project(jobs, (job) => job.label() + \":\" + job.stage.decorate(job.name))\n" +
		"println(labels[0])\n" +
		"println(labels[1])\n" +
		"println(countMatches(jobs, (job) => job.stage == Stage.DONE))\n" +
		"let byName = HashMap<string, Job>()\n" +
		"for job in jobs:\n" +
		"    byName.put(job.name, job)\n" +
		"end\n" +
		"let ship: Job = byName.get(\"ship\")\n" +
		"let pair: Pair<string, string> = Pair(ship.name, ship.stage.name)\n" +
		"let running = Stage.valueOf(\"RUNNING\")\n" +
		"println(pair[0] + \":\" + pair[1])\n" +
		"println(Stage.names()[2])\n" +
		"println(running.ordinal)\n" +
		"println(Stage.valueOf(\"READY\").decorate(\"boot\"))\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "build@READY:build:1\nship@DONE:ship:3\n1\nship:DONE\nDONE\n1\nboot:1\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestInlineBytesEqualsConstructorArgumentWorks(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Bytes }\n" +
		"let fromHex = Bytes.fromHex(\"0x48656C6C6F\")\n" +
		"println(fromHex.equals(Bytes([72, 101, 108, 108, 111])))\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestToStringDrivesPrintlnAndStringConcatenation(t *testing.T) {
	source := `class Person:
    let name: string

    Person(name: string):
        this.name = name
    end

    def toString() -> string:
        return "Person(" + this.name + ")"
    end
end

let person = Person("Ada")
println(person)
println(person + "!")
println(">" + person)
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
	if got, want := out.String(), "Person(Ada)\nPerson(Ada)!\n>Person(Ada)\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestOriginalEnumAndRecordScenarioPort(t *testing.T) {
	source := `enum Color
    RED
    GREEN
    BLUE
end

record Point(x: number, y: number)
    def sum() -> number:
        return this.x + this.y
    end
end

let green = Color.valueOf("GREEN")
println(green.name)
println(green.ordinal)

let point = Point(2, 5)
println(point.x)
println(point.y)
println(point.sum())

enum Planet
    MERCURY(3.7)
    MARS(3.71)

    var gravity: number

    Planet(g: number):
        this.gravity = g
    end

    def weight(mass: number) -> number:
        return mass * this.gravity
    end
end

println(Planet.MARS.gravity)
println(Planet.MARS.weight(10.0))
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
	if got, want := out.String(), "GREEN\n1\n2\n5\n7\n3.71\n37.1\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestOriginalSealedEnumHelpersPort(t *testing.T) {
	source := `sealed enum Mode
    OFF
    ON
end

let names: array<string> = Mode.names()
let values: array<Mode> = Mode.values()
println(names[0])
println(values[1].name)
println(Mode.size())
println(Mode.valueOf("ON").ordinal)
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
	if got, want := out.String(), "OFF\nON\n2\n1\n"; got != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, got)
	}
}

func TestSysTypeAndInstanceOfBuiltins(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Integer }\n" +
		"class Animal:\n" +
		"end\n\n" +
		"class Dog extends Animal:\n" +
		"    def bark() -> string:\n" +
		"        return \"woof\"\n" +
		"    end\n" +
		"end\n\n" +
		"println(Sys.type(Dog))\n" +
		"println(Sys.type(Dog()))\n" +
		"println(Sys.type(5))\n" +
		"println(Sys.instanceof(Dog(), Dog))\n" +
		"println(Sys.instanceof(Dog(), Animal))\n" +
		"println(Sys.instanceof(5, Integer))\n" +
		"println(Sys.instanceof(\"hi\", \"string\"))\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "Class Dog\nDog\nint\ntrue\ntrue\ntrue\ntrue\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestInstanceOfOperatorAndBinding(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Integer }\n" +
		"class Animal:\n" +
		"end\n\n" +
		"class Dog extends Animal:\n" +
		"    def bark() -> string:\n" +
		"        return \"woof\"\n" +
		"    end\n" +
		"end\n\n" +
		"let count: any = 5\n" +
		"let pet: Animal = Dog()\n\n" +
		"println(count instanceof Integer)\n" +
		"println(pet instanceof Animal)\n\n" +
		"if pet instanceof Dog dog:\n" +
		"    println(dog.bark())\n" +
		"else:\n" +
		"    println(\"no\")\n" +
		"end\n\n" +
		"if count instanceof Integer integer:\n" +
		"    println(Sys.type(integer))\n" +
		"else:\n" +
		"    println(0)\n" +
		"end\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "true\ntrue\nwoof\nInteger\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestVariadicPrintAndStringBuilderFeatures(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { String, StringBuilder, Integer }\n" +
		"println(\"a\", 1)\n" +
		"println(String.format(\"hola {} {}\", [\"mundo\", 7]))\n" +
		"let sb = StringBuilder(\"\")\n" +
		"sb.append(\"A\").append(1).appendLine(\"!\")\n" +
		"println(sb.toString())\n" +
		"println(\"x\", Integer(2), [3])\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "a 1\nhola mundo 7\nA1!\n\nx 2 [3]\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}

func TestInstanceOfBindingInComposedIfAndForWhere(t *testing.T) {
	tempDir := t.TempDir()
	mainPath := filepath.Join(tempDir, "main.pf")
	mainSource := "import polyloft.common { Integer }\n" +
		"class Animal:\n" +
		"end\n\n" +
		"class Dog extends Animal:\n" +
		"    def bark() -> string:\n" +
		"        return \"woof\"\n" +
		"    end\n" +
		"end\n\n" +
		"let pet: Animal = Dog()\n" +
		"if pet instanceof Dog dog && dog.bark() == \"woof\":\n" +
		"    println(\"ok\")\n" +
		"else:\n" +
		"    println(\"bad\")\n" +
		"end\n\n" +
		"for a in [1, \"2\", [3], 4] where a instanceof Integer b:\n" +
		"    println(Sys.type(b), b)\n" +
		"end\n"
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write main failed: %v", err)
	}
	output, err := runPreparedFile(t, mainPath)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if want := "ok\nInteger 1\nInteger 4\n"; output != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, output)
	}
}
