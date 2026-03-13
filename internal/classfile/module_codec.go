package classfile

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

const (
	cpUtf8    = 1
	cpInteger = 3
	cpFloat   = 4
	cpLong    = 5
	cpDouble  = 6
	cpClass   = 7
	cpString  = 8

	accPublic = 0x0001
	accFinal  = 0x0010
	accStatic = 0x0008

	constNil byte = iota
	constInt64
	constFloat64
	constString
	constRune
	constBool
	constFunctionRef
	constClassRef
)

type cpEntry struct {
	tag        byte
	utf8       string
	int32Val   int32
	int64Val   int64
	float32Val float32
	float64Val float64
	ref1       uint16
	ref2       uint16
}

type cpBuilder struct {
	entries        []cpEntry
	utf8           map[string]uint16
	strings        map[string]uint16
	classes        map[string]uint16
	int32s         map[int32]uint16
	int64s         map[int64]uint16
	float64s       map[uint64]uint16
	attributeNames map[string]uint16
}

func newCPBuilder() *cpBuilder {
	return &cpBuilder{
		entries:        make([]cpEntry, 0, 64),
		utf8:           make(map[string]uint16),
		strings:        make(map[string]uint16),
		classes:        make(map[string]uint16),
		int32s:         make(map[int32]uint16),
		int64s:         make(map[int64]uint16),
		float64s:       make(map[uint64]uint16),
		attributeNames: make(map[string]uint16),
	}
}

func (cp *cpBuilder) add(entry cpEntry) uint16 {
	cp.entries = append(cp.entries, entry)
	index := uint16(len(cp.entries))
	if entry.tag == cpLong || entry.tag == cpDouble {
		cp.entries = append(cp.entries, cpEntry{})
	}
	return index
}

func (cp *cpBuilder) UTF8(value string) uint16 {
	if idx, ok := cp.utf8[value]; ok {
		return idx
	}
	idx := cp.add(cpEntry{tag: cpUtf8, utf8: value})
	cp.utf8[value] = idx
	return idx
}

func (cp *cpBuilder) AttributeName(value string) uint16 {
	if idx, ok := cp.attributeNames[value]; ok {
		return idx
	}
	idx := cp.UTF8(value)
	cp.attributeNames[value] = idx
	return idx
}

func (cp *cpBuilder) String(value string) uint16 {
	if idx, ok := cp.strings[value]; ok {
		return idx
	}
	utf8Idx := cp.UTF8(value)
	idx := cp.add(cpEntry{tag: cpString, ref1: utf8Idx})
	cp.strings[value] = idx
	return idx
}

func (cp *cpBuilder) Class(name string) uint16 {
	if idx, ok := cp.classes[name]; ok {
		return idx
	}
	utf8Idx := cp.UTF8(name)
	idx := cp.add(cpEntry{tag: cpClass, ref1: utf8Idx})
	cp.classes[name] = idx
	return idx
}

func (cp *cpBuilder) Int32(value int32) uint16 {
	if idx, ok := cp.int32s[value]; ok {
		return idx
	}
	idx := cp.add(cpEntry{tag: cpInteger, int32Val: value})
	cp.int32s[value] = idx
	return idx
}

func (cp *cpBuilder) Int64(value int64) uint16 {
	if idx, ok := cp.int64s[value]; ok {
		return idx
	}
	idx := cp.add(cpEntry{tag: cpLong, int64Val: value})
	cp.int64s[value] = idx
	return idx
}

func (cp *cpBuilder) Float64(value float64) uint16 {
	bits := math.Float64bits(value)
	if idx, ok := cp.float64s[bits]; ok {
		return idx
	}
	idx := cp.add(cpEntry{tag: cpDouble, float64Val: value})
	cp.float64s[bits] = idx
	return idx
}

func writeCP(w io.Writer, entries []cpEntry) error {
	if err := binary.Write(w, binary.BigEndian, uint16(len(entries)+1)); err != nil {
		return err
	}
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if entry.tag == 0 {
			continue
		}
		if err := writeU1(w, entry.tag); err != nil {
			return err
		}
		switch entry.tag {
		case cpUtf8:
			if err := writeUTF8(w, entry.utf8); err != nil {
				return err
			}
		case cpInteger:
			if err := binary.Write(w, binary.BigEndian, entry.int32Val); err != nil {
				return err
			}
		case cpFloat:
			if err := binary.Write(w, binary.BigEndian, math.Float32bits(entry.float32Val)); err != nil {
				return err
			}
		case cpLong:
			if err := binary.Write(w, binary.BigEndian, entry.int64Val); err != nil {
				return err
			}
			i++
		case cpDouble:
			if err := binary.Write(w, binary.BigEndian, math.Float64bits(entry.float64Val)); err != nil {
				return err
			}
			i++
		case cpClass, cpString:
			if err := writeU2(w, entry.ref1); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported constant pool tag %d", entry.tag)
		}
	}
	return nil
}

type cpReader struct {
	entries []cpEntry
}

func readCP(r io.Reader) (*cpReader, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	entries := make([]cpEntry, int(count-1))
	for i := 1; i < int(count); i++ {
		entryIndex := i - 1
		tag, err := readU1(r)
		if err != nil {
			return nil, err
		}
		entry := cpEntry{tag: tag}
		switch tag {
		case cpUtf8:
			entry.utf8, err = readUTF8(r)
		case cpInteger:
			err = binary.Read(r, binary.BigEndian, &entry.int32Val)
		case cpFloat:
			var bits uint32
			err = binary.Read(r, binary.BigEndian, &bits)
			entry.float32Val = math.Float32frombits(bits)
		case cpLong:
			err = binary.Read(r, binary.BigEndian, &entry.int64Val)
			if err == nil && i < int(count)-1 {
				entries[i] = cpEntry{}
				i++
			}
		case cpDouble:
			var bits uint64
			err = binary.Read(r, binary.BigEndian, &bits)
			entry.float64Val = math.Float64frombits(bits)
			if err == nil && i < int(count)-1 {
				entries[i] = cpEntry{}
				i++
			}
		case cpClass, cpString:
			entry.ref1, err = readU2(r)
		default:
			return nil, fmt.Errorf("unsupported constant pool tag %d", tag)
		}
		if err != nil {
			return nil, err
		}
		entries[entryIndex] = entry
	}
	return &cpReader{entries: entries}, nil
}

func (cp *cpReader) entry(index uint16) (cpEntry, error) {
	if index == 0 || int(index) > len(cp.entries) {
		return cpEntry{}, fmt.Errorf("invalid constant pool index %d", index)
	}
	entry := cp.entries[index-1]
	if entry.tag == 0 {
		return cpEntry{}, fmt.Errorf("constant pool index %d is reserved", index)
	}
	return entry, nil
}

func (cp *cpReader) UTF8(index uint16) (string, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return "", err
	}
	if entry.tag != cpUtf8 {
		return "", fmt.Errorf("constant pool index %d is not Utf8", index)
	}
	return entry.utf8, nil
}

func (cp *cpReader) Class(index uint16) (string, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return "", err
	}
	if entry.tag != cpClass {
		return "", fmt.Errorf("constant pool index %d is not Class", index)
	}
	return cp.UTF8(entry.ref1)
}

func (cp *cpReader) String(index uint16) (string, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return "", err
	}
	if entry.tag != cpString {
		return "", fmt.Errorf("constant pool index %d is not String", index)
	}
	return cp.UTF8(entry.ref1)
}

func (cp *cpReader) Int32(index uint16) (int32, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return 0, err
	}
	if entry.tag != cpInteger {
		return 0, fmt.Errorf("constant pool index %d is not Integer", index)
	}
	return entry.int32Val, nil
}

func (cp *cpReader) Int64(index uint16) (int64, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return 0, err
	}
	if entry.tag != cpLong {
		return 0, fmt.Errorf("constant pool index %d is not Long", index)
	}
	return entry.int64Val, nil
}

func (cp *cpReader) Float64(index uint16) (float64, error) {
	entry, err := cp.entry(index)
	if err != nil {
		return 0, err
	}
	if entry.tag != cpDouble {
		return 0, fmt.Errorf("constant pool index %d is not Double", index)
	}
	return entry.float64Val, nil
}

type encodedConstant struct {
	kind     byte
	cpIndex  uint16
	refIndex uint16
}

type encodedMethod struct {
	nameIndex       uint16
	descriptorIndex uint16
	accessFlags     uint16
	code            []byte
	lines           []int
	ownerClassName  string
	upvalues        []bytecode.UpvalueSpec
	returnType      string
	maxLocals       uint16
	globalSlotCount uint16
	globalSlotNames []string
	interfaces      map[string]bytecode.InterfaceSpec
	constants       []encodedConstant
}

type encodedFieldDef struct {
	name       string
	typeName   string
	visibility string
	mutable    bool
	isFinal    bool
}

type encodedMethodBinding struct {
	name        string
	methodIndex uint16
}

type encodedSpecialMethod struct {
	slot        uint8
	methodIndex uint16
}

type encodedClass struct {
	id                    uint16
	name                  string
	superclassID          uint16
	superclassName        string
	implements            []string
	permits               []string
	flags                 uint16
	enumOrder             []string
	fields                []encodedFieldDef
	fieldOrder            []string
	fieldSlots            []encodedMethodBinding
	staticFields          []encodedFieldDef
	methodOrder           []string
	methodSlots           []encodedMethodBinding
	methodOverloads       map[string][]uint16
	constructor           uint16
	constructorOverloads  []uint16
	staticMethods         []encodedMethodBinding
	staticMethodOverloads map[string][]uint16
	specialMethods        []encodedSpecialMethod
	methodVisibility      map[string]string
	staticVisibility      map[string]string
	methodAnnotations     map[string][]string
	constructorVisibility string
}

type moduleState struct {
	cp            *cpBuilder
	functions     []*bytecode.Function
	functionIndex map[*bytecode.Function]uint16
	classes       []*value.Class
	classIndex    map[*value.Class]uint16
	fields        []fieldInfoWire
	methods       []encodedMethod
	classesAttr   []encodedClass
	entryMethod   uint16
	metadata      []byte
}

type fieldInfoWire struct {
	accessFlags     uint16
	nameIndex       uint16
	descriptorIndex uint16
}

func buildModuleState(fn *bytecode.Function, metadata []byte) (*moduleState, error) {
	state := &moduleState{
		cp:            newCPBuilder(),
		functions:     make([]*bytecode.Function, 0, 16),
		functionIndex: make(map[*bytecode.Function]uint16),
		classes:       make([]*value.Class, 0, 8),
		classIndex:    make(map[*value.Class]uint16),
		fields:        make([]fieldInfoWire, 0),
		methods:       make([]encodedMethod, 0, 16),
		classesAttr:   make([]encodedClass, 0, 8),
		metadata:      metadata,
	}
	state.cp.AttributeName("Code")
	state.cp.AttributeName("LineNumberTable")
	state.cp.AttributeName("PolyloftMethod")
	state.cp.AttributeName("PolyloftModule")
	state.cp.AttributeName("PolyloftClasses")
	state.cp.AttributeName("PolyloftMetadata")
	collectReachableFunctions(state, fn)
	state.entryMethod = state.functionIndex[fn]
	state.fields = encodeGlobalFields(state, fn)
	for _, f := range state.functions {
		method, err := encodeMethod(state, f)
		if err != nil {
			return nil, err
		}
		state.methods = append(state.methods, method)
	}
	for _, classValue := range state.classes {
		state.classesAttr = append(state.classesAttr, encodeClass(state, classValue))
	}
	return state, nil
}

func collectReachableFunctions(state *moduleState, fn *bytecode.Function) {
	if fn == nil {
		return
	}
	if _, ok := state.functionIndex[fn]; ok {
		return
	}
	state.functions = append(state.functions, fn)
	state.functionIndex[fn] = uint16(len(state.functions))
	if fn.Chunk == nil {
		return
	}
	for _, constant := range fn.Chunk.Constants {
		switch valueConst := constant.(type) {
		case *bytecode.Function:
			collectReachableFunctions(state, valueConst)
		case *value.Class:
			collectReachableClass(state, valueConst)
		}
	}
}

func collectReachableClass(state *moduleState, classValue *value.Class) {
	if classValue == nil {
		return
	}
	if _, ok := state.classIndex[classValue]; ok {
		return
	}
	state.classes = append(state.classes, classValue)
	state.classIndex[classValue] = uint16(len(state.classes))
	collectReachableClass(state, classValue.Superclass)
	for _, fn := range classValue.Methods {
		collectReachableFunctions(state, fn)
	}
	for _, overloads := range classValue.MethodOverloads {
		for _, fn := range overloads {
			collectReachableFunctions(state, fn)
		}
	}
	for _, fn := range classValue.StaticMethods {
		collectReachableFunctions(state, fn)
	}
	for _, overloads := range classValue.StaticMethodOverloads {
		for _, fn := range overloads {
			collectReachableFunctions(state, fn)
		}
	}
	collectReachableFunctions(state, classValue.Constructor)
	for _, fn := range classValue.ConstructorOverloads {
		collectReachableFunctions(state, fn)
	}
	for _, fn := range classValue.SpecialMethods {
		collectReachableFunctions(state, fn)
	}
}

func encodeGlobalFields(state *moduleState, fn *bytecode.Function) []fieldInfoWire {
	if fn == nil {
		return nil
	}
	fields := make([]fieldInfoWire, 0, len(fn.GlobalSlotNames))
	seen := make(map[string]bool)
	for _, name := range fn.GlobalSlotNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		fields = append(fields, fieldInfoWire{
			accessFlags:     accPublic | accStatic,
			nameIndex:       state.cp.UTF8(name),
			descriptorIndex: state.cp.UTF8(typeDescriptor("Any")),
		})
	}
	return fields
}

func encodeMethod(state *moduleState, fn *bytecode.Function) (encodedMethod, error) {
	method := encodedMethod{
		nameIndex:       state.cp.UTF8(methodName(fn)),
		descriptorIndex: state.cp.UTF8(methodDescriptor(fn.ParamTypes, fn.ReturnType)),
		accessFlags:     accPublic | accStatic,
		ownerClassName:  fn.OwnerClassName,
		upvalues:        append([]bytecode.UpvalueSpec(nil), fn.Upvalues...),
		returnType:      fn.ReturnType,
		maxLocals:       uint16(fn.MaxLocals),
		globalSlotCount: uint16(fn.GlobalSlotCount),
		globalSlotNames: append([]string(nil), fn.GlobalSlotNames...),
		interfaces:      cloneInterfaces(fn.Interfaces),
	}
	if fn.Chunk != nil {
		method.code = append([]byte(nil), fn.Chunk.Code...)
		method.lines = append([]int(nil), fn.Chunk.Lines...)
		method.constants = make([]encodedConstant, 0, len(fn.Chunk.Constants))
		for _, constant := range fn.Chunk.Constants {
			encoded, err := encodeConstant(state, constant)
			if err != nil {
				return encodedMethod{}, err
			}
			method.constants = append(method.constants, encoded)
		}
	}
	return method, nil
}

func encodeConstant(state *moduleState, constant any) (encodedConstant, error) {
	switch valueConst := constant.(type) {
	case nil:
		return encodedConstant{kind: constNil}, nil
	case int64:
		return encodedConstant{kind: constInt64, cpIndex: state.cp.Int64(valueConst)}, nil
	case float64:
		return encodedConstant{kind: constFloat64, cpIndex: state.cp.Float64(valueConst)}, nil
	case string:
		return encodedConstant{kind: constString, cpIndex: state.cp.String(valueConst)}, nil
	case rune:
		return encodedConstant{kind: constRune, cpIndex: state.cp.Int32(int32(valueConst))}, nil
	case bool:
		if valueConst {
			return encodedConstant{kind: constBool, cpIndex: state.cp.Int32(1)}, nil
		}
		return encodedConstant{kind: constBool, cpIndex: state.cp.Int32(0)}, nil
	case *bytecode.Function:
		return encodedConstant{kind: constFunctionRef, refIndex: state.functionIndex[valueConst]}, nil
	case *value.Class:
		return encodedConstant{kind: constClassRef, refIndex: state.classIndex[valueConst]}, nil
	default:
		return encodedConstant{}, fmt.Errorf("classfile: unsupported constant %T", constant)
	}
}

func encodeClass(state *moduleState, classValue *value.Class) encodedClass {
	encoded := encodedClass{
		id:                    state.classIndex[classValue],
		name:                  classValue.Name,
		superclassID:          state.classIndex[classValue.Superclass],
		implements:            sortedMapKeys(classValue.Implements),
		permits:               sortedMapKeys(classValue.Permits),
		flags:                 classFlags(classValue),
		enumOrder:             append([]string(nil), classValue.EnumOrder...),
		fields:                encodeFieldDefs(classValue.Fields),
		fieldOrder:            append([]string(nil), classValue.FieldOrder...),
		fieldSlots:            encodeFieldSlots(classValue.FieldIndex),
		staticFields:          encodeFieldDefs(classValue.StaticFields),
		methodOrder:           append([]string(nil), classValue.MethodOrder...),
		methodSlots:           encodeMethodBindings(classValue.Methods, state.functionIndex),
		methodOverloads:       encodeOverloads(classValue.MethodOverloads, state.functionIndex),
		constructor:           state.functionIndex[classValue.Constructor],
		constructorOverloads:  encodeOverloadSlice(classValue.ConstructorOverloads, state.functionIndex),
		staticMethods:         encodeMethodBindings(classValue.StaticMethods, state.functionIndex),
		staticMethodOverloads: encodeOverloads(classValue.StaticMethodOverloads, state.functionIndex),
		specialMethods:        encodeSpecialMethods(classValue.SpecialMethods, state.functionIndex),
		methodVisibility:      cloneStringMap(classValue.MethodVisibility),
		staticVisibility:      cloneStringMap(classValue.StaticVisibility),
		methodAnnotations:     cloneAnnotations(classValue.MethodAnnotations),
		constructorVisibility: classValue.ConstructorVisibility,
	}
	if classValue.Superclass != nil {
		encoded.superclassName = classValue.Superclass.Name
	}
	return encoded
}

func methodName(fn *bytecode.Function) string {
	if fn == nil || fn.Name == "" {
		return "<script>"
	}
	return fn.Name
}

func cloneInterfaces(src map[string]bytecode.InterfaceSpec) map[string]bytecode.InterfaceSpec {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]bytecode.InterfaceSpec, len(src))
	for key, spec := range src {
		methods := make(map[string]bytecode.InterfaceMethodSpec, len(spec.Methods))
		for methodName, methodSpec := range spec.Methods {
			methods[methodName] = bytecode.InterfaceMethodSpec{
				Name:   methodSpec.Name,
				Params: append([]string(nil), methodSpec.Params...),
				Return: methodSpec.Return,
			}
		}
		dst[key] = bytecode.InterfaceSpec{Name: spec.Name, Methods: methods, FunctionalMethod: spec.FunctionalMethod}
	}
	return dst
}

func encodeFieldDefs(src map[string]value.FieldDef) []encodedFieldDef {
	if len(src) == 0 {
		return nil
	}
	names := make([]string, 0, len(src))
	for name := range src {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]encodedFieldDef, 0, len(names))
	for _, name := range names {
		field := src[name]
		fields = append(fields, encodedFieldDef{name: name, typeName: field.TypeName, visibility: field.Visibility, mutable: field.Mutable, isFinal: field.IsFinal})
	}
	return fields
}

func encodeFieldSlots(src map[string]int) []encodedMethodBinding {
	if len(src) == 0 {
		return nil
	}
	names := make([]string, 0, len(src))
	for name := range src {
		names = append(names, name)
	}
	sort.Strings(names)
	slots := make([]encodedMethodBinding, 0, len(names))
	for _, name := range names {
		slots = append(slots, encodedMethodBinding{name: name, methodIndex: uint16(src[name])})
	}
	return slots
}

func encodeMethodBindings(src map[string]*bytecode.Function, indexes map[*bytecode.Function]uint16) []encodedMethodBinding {
	if len(src) == 0 {
		return nil
	}
	names := make([]string, 0, len(src))
	for name := range src {
		names = append(names, name)
	}
	sort.Strings(names)
	bindings := make([]encodedMethodBinding, 0, len(names))
	for _, name := range names {
		bindings = append(bindings, encodedMethodBinding{name: name, methodIndex: indexes[src[name]]})
	}
	return bindings
}

func encodeOverloads(src map[string][]*bytecode.Function, indexes map[*bytecode.Function]uint16) map[string][]uint16 {
	if len(src) == 0 {
		return nil
	}
	result := make(map[string][]uint16, len(src))
	for name, overloads := range src {
		result[name] = encodeOverloadSlice(overloads, indexes)
	}
	return result
}

func encodeOverloadSlice(src []*bytecode.Function, indexes map[*bytecode.Function]uint16) []uint16 {
	if len(src) == 0 {
		return nil
	}
	result := make([]uint16, 0, len(src))
	for _, fn := range src {
		if fn == nil {
			continue
		}
		result = append(result, indexes[fn])
	}
	return result
}

func encodeSpecialMethods(src map[value.SpecialMethodSlot]*bytecode.Function, indexes map[*bytecode.Function]uint16) []encodedSpecialMethod {
	if len(src) == 0 {
		return nil
	}
	methods := make([]encodedSpecialMethod, 0, len(src))
	for _, slot := range specialMethodSlots() {
		fn, ok := src[slot]
		if !ok || fn == nil {
			continue
		}
		methods = append(methods, encodedSpecialMethod{slot: uint8(slot), methodIndex: indexes[fn]})
	}
	return methods
}

func specialMethodSlots() []value.SpecialMethodSlot {
	return []value.SpecialMethodSlot{
		value.SpecialMethodIterableLength,
		value.SpecialMethodIterableGet,
		value.SpecialMethodPieces,
		value.SpecialMethodGetPiece,
		value.SpecialMethodIndexGet,
		value.SpecialMethodIndexSet,
		value.SpecialMethodContains,
		value.SpecialMethodSlice,
		value.SpecialMethodEquals,
		value.SpecialMethodHash,
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneAnnotations(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func sortedMapKeys[T ~bool](src map[string]T) []string {
	if len(src) == 0 {
		return nil
	}
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func classFlags(classValue *value.Class) uint16 {
	flags := uint16(accPublic)
	if classValue.IsSealed || classValue.IsEnum {
		flags |= accFinal
	}
	return flags
}

func moduleInternalName(fn *bytecode.Function) string {
	name := "polyloft/Module"
	if fn != nil && fn.OwnerClassName != "" {
		name = toInternalName(fn.OwnerClassName)
	}
	return name
}

func toInternalName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "polyloft/Any"
	}
	return strings.ReplaceAll(name, ".", "/")
}

func methodDescriptor(params []string, ret string) string {
	var builder strings.Builder
	builder.WriteByte('(')
	for _, param := range params {
		builder.WriteString(typeDescriptor(param))
	}
	builder.WriteByte(')')
	builder.WriteString(typeDescriptor(ret))
	return builder.String()
}

func typeDescriptor(name string) string {
	switch strings.TrimSpace(name) {
	case "", "Void", "void":
		return "V"
	case "Int", "int":
		return "I"
	case "Float", "float", "Number", "number":
		return "D"
	case "Bool", "bool", "Boolean", "boolean":
		return "Z"
	case "Char", "char":
		return "C"
	case "String", "string":
		return "Ljava/lang/String;"
	case "Any", "any":
		return "Lpolyloft/Any;"
	case "Function":
		return "Lpolyloft/Function;"
	case "Array", "array":
		return "Lpolyloft/Array;"
	case "Map", "map":
		return "Lpolyloft/Map;"
	case "Tuple", "tuple":
		return "Lpolyloft/Tuple;"
	case "Range", "range":
		return "Lpolyloft/Range;"
	case "Module", "module":
		return "Lpolyloft/Module;"
	case "Nil", "nil":
		return "Lpolyloft/Nil;"
	default:
		trimmed := strings.TrimSpace(name)
		if strings.HasSuffix(trimmed, "[]") {
			return "[" + typeDescriptor(strings.TrimSuffix(trimmed, "[]"))
		}
		return "L" + toInternalName(trimmed) + ";"
	}
}

func writeModuleClassFile(w io.Writer, fn *bytecode.Function, metadata []byte) error {
	state, err := buildModuleState(fn, metadata)
	if err != nil {
		return err
	}
	thisClass := state.cp.Class(moduleInternalName(fn))
	if err := writeU4(w, Magic); err != nil {
		return err
	}
	if err := writeU2(w, MinorVersion); err != nil {
		return err
	}
	if err := writeU2(w, MajorVersion); err != nil {
		return err
	}
	if err := writeCP(w, state.cp.entries); err != nil {
		return err
	}
	if err := writeU2(w, accPublic|accFinal); err != nil {
		return err
	}
	if err := writeU2(w, thisClass); err != nil {
		return err
	}
	if err := writeU2(w, 0); err != nil {
		return err
	}
	if err := writeU2(w, 0); err != nil {
		return err
	}
	if err := writeFields(w, state.fields); err != nil {
		return err
	}
	if err := writeMethods(w, state.cp, state.methods); err != nil {
		return err
	}
	attributes, err := buildClassAttributes(state.cp, state.entryMethod, state.classesAttr, metadata)
	if err != nil {
		return err
	}
	if err := writeU2(w, uint16(len(attributes))); err != nil {
		return err
	}
	for _, attr := range attributes {
		if err := writeAttribute(w, attr); err != nil {
			return err
		}
	}
	return nil
}

type attributeWire struct {
	nameIndex uint16
	info      []byte
}

func buildClassAttributes(cp *cpBuilder, entryMethod uint16, classes []encodedClass, metadata []byte) ([]attributeWire, error) {
	attrs := make([]attributeWire, 0, 3)
	moduleInfo := new(bytes.Buffer)
	if err := writeU2(moduleInfo, entryMethod); err != nil {
		return nil, err
	}
	attrs = append(attrs, attributeWire{nameIndex: cp.AttributeName("PolyloftModule"), info: moduleInfo.Bytes()})
	classesInfo, err := encodeClassesAttribute(classes)
	if err != nil {
		return nil, err
	}
	attrs = append(attrs, attributeWire{nameIndex: cp.AttributeName("PolyloftClasses"), info: classesInfo})
	attrs = append(attrs, attributeWire{nameIndex: cp.AttributeName("PolyloftMetadata"), info: metadata})
	return attrs, nil
}

func writeFields(w io.Writer, fields []fieldInfoWire) error {
	if err := writeU2(w, uint16(len(fields))); err != nil {
		return err
	}
	for _, field := range fields {
		if err := writeU2(w, field.accessFlags); err != nil {
			return err
		}
		if err := writeU2(w, field.nameIndex); err != nil {
			return err
		}
		if err := writeU2(w, field.descriptorIndex); err != nil {
			return err
		}
		if err := writeU2(w, 0); err != nil {
			return err
		}
	}
	return nil
}

func writeMethods(w io.Writer, cp *cpBuilder, methods []encodedMethod) error {
	if err := writeU2(w, uint16(len(methods))); err != nil {
		return err
	}
	for _, method := range methods {
		if err := writeU2(w, method.accessFlags); err != nil {
			return err
		}
		if err := writeU2(w, method.nameIndex); err != nil {
			return err
		}
		if err := writeU2(w, method.descriptorIndex); err != nil {
			return err
		}
		attributes, err := buildMethodAttributes(cp, method)
		if err != nil {
			return err
		}
		if err := writeU2(w, uint16(len(attributes))); err != nil {
			return err
		}
		for _, attr := range attributes {
			if err := writeAttribute(w, attr); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildMethodAttributes(cp *cpBuilder, method encodedMethod) ([]attributeWire, error) {
	codeAttr, err := encodeCodeAttribute(cp, method)
	if err != nil {
		return nil, err
	}
	methodMeta, err := encodeMethodMeta(method)
	if err != nil {
		return nil, err
	}
	return []attributeWire{
		{nameIndex: cp.AttributeName("Code"), info: codeAttr},
		{nameIndex: cp.AttributeName("PolyloftMethod"), info: methodMeta},
	}, nil
}

func encodeCodeAttribute(cp *cpBuilder, method encodedMethod) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := writeU2(buf, 0); err != nil {
		return nil, err
	}
	if err := writeU2(buf, method.maxLocals); err != nil {
		return nil, err
	}
	if err := writeU4(buf, uint32(len(method.code))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(method.code); err != nil {
		return nil, err
	}
	if err := writeU2(buf, 0); err != nil {
		return nil, err
	}
	lineInfo, err := encodeLineNumberTable(method.lines)
	if err != nil {
		return nil, err
	}
	attrs := []attributeWire{{nameIndex: cp.AttributeName("LineNumberTable"), info: lineInfo}}
	if err := writeU2(buf, uint16(len(attrs))); err != nil {
		return nil, err
	}
	for _, attr := range attrs {
		if err := writeAttribute(buf, attr); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeLineNumberTable(lines []int) ([]byte, error) {
	buf := new(bytes.Buffer)
	entries := compactLines(lines)
	if err := writeU2(buf, uint16(len(entries))); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := writeU2(buf, uint16(entry.offset)); err != nil {
			return nil, err
		}
		if err := writeU2(buf, uint16(entry.line)); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

type lineEntry struct {
	offset int
	line   int
}

func compactLines(lines []int) []lineEntry {
	if len(lines) == 0 {
		return nil
	}
	entries := make([]lineEntry, 0, len(lines))
	lastLine := -1
	for offset, line := range lines {
		if offset == 0 || line != lastLine {
			entries = append(entries, lineEntry{offset: offset, line: line})
			lastLine = line
		}
	}
	return entries
}

func encodeMethodMeta(method encodedMethod) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := writeUTF8(buf, method.ownerClassName); err != nil {
		return nil, err
	}
	if err := writeUTF8(buf, method.returnType); err != nil {
		return nil, err
	}
	if err := writeU2(buf, uint16(len(method.upvalues))); err != nil {
		return nil, err
	}
	for _, upvalue := range method.upvalues {
		if err := writeU1(buf, upvalue.Index); err != nil {
			return nil, err
		}
		var isLocal byte
		if upvalue.IsLocal {
			isLocal = 1
		}
		if err := writeU1(buf, isLocal); err != nil {
			return nil, err
		}
	}
	if err := writeU2(buf, method.globalSlotCount); err != nil {
		return nil, err
	}
	if err := writeStringList(buf, method.globalSlotNames); err != nil {
		return nil, err
	}
	if err := encodeInterfaces(buf, method.interfaces); err != nil {
		return nil, err
	}
	if err := writeU2(buf, uint16(len(method.constants))); err != nil {
		return nil, err
	}
	for _, constant := range method.constants {
		if err := writeU1(buf, constant.kind); err != nil {
			return nil, err
		}
		if err := writeU2(buf, constant.cpIndex); err != nil {
			return nil, err
		}
		if err := writeU2(buf, constant.refIndex); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func encodeInterfaces(w io.Writer, interfaces map[string]bytecode.InterfaceSpec) error {
	if err := writeU2(w, uint16(len(interfaces))); err != nil {
		return err
	}
	names := make([]string, 0, len(interfaces))
	for name := range interfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec := interfaces[name]
		if err := writeUTF8(w, name); err != nil {
			return err
		}
		if err := writeUTF8(w, spec.Name); err != nil {
			return err
		}
		if err := writeUTF8(w, spec.FunctionalMethod); err != nil {
			return err
		}
		methodNames := make([]string, 0, len(spec.Methods))
		for methodName := range spec.Methods {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)
		if err := writeU2(w, uint16(len(methodNames))); err != nil {
			return err
		}
		for _, methodName := range methodNames {
			method := spec.Methods[methodName]
			if err := writeUTF8(w, methodName); err != nil {
				return err
			}
			if err := writeUTF8(w, method.Name); err != nil {
				return err
			}
			if err := writeStringList(w, method.Params); err != nil {
				return err
			}
			if err := writeUTF8(w, method.Return); err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeClassesAttribute(classes []encodedClass) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := writeU2(buf, uint16(len(classes))); err != nil {
		return nil, err
	}
	for _, classInfo := range classes {
		if err := writeU2(buf, classInfo.id); err != nil {
			return nil, err
		}
		if err := writeUTF8(buf, classInfo.name); err != nil {
			return nil, err
		}
		if err := writeU2(buf, classInfo.superclassID); err != nil {
			return nil, err
		}
		if err := writeUTF8(buf, classInfo.superclassName); err != nil {
			return nil, err
		}
		if err := writeU2(buf, classInfo.flags); err != nil {
			return nil, err
		}
		if err := writeStringList(buf, classInfo.implements); err != nil {
			return nil, err
		}
		if err := writeStringList(buf, classInfo.permits); err != nil {
			return nil, err
		}
		if err := writeStringList(buf, classInfo.enumOrder); err != nil {
			return nil, err
		}
		if err := writeEncodedFieldDefs(buf, classInfo.fields); err != nil {
			return nil, err
		}
		if err := writeStringList(buf, classInfo.fieldOrder); err != nil {
			return nil, err
		}
		if err := writeMethodBindings(buf, classInfo.fieldSlots); err != nil {
			return nil, err
		}
		if err := writeEncodedFieldDefs(buf, classInfo.staticFields); err != nil {
			return nil, err
		}
		if err := writeStringList(buf, classInfo.methodOrder); err != nil {
			return nil, err
		}
		if err := writeMethodBindings(buf, classInfo.methodSlots); err != nil {
			return nil, err
		}
		if err := writeOverloads(buf, classInfo.methodOverloads); err != nil {
			return nil, err
		}
		if err := writeU2(buf, classInfo.constructor); err != nil {
			return nil, err
		}
		if err := writeU2List(buf, classInfo.constructorOverloads); err != nil {
			return nil, err
		}
		if err := writeMethodBindings(buf, classInfo.staticMethods); err != nil {
			return nil, err
		}
		if err := writeOverloads(buf, classInfo.staticMethodOverloads); err != nil {
			return nil, err
		}
		if err := writeSpecialMethods(buf, classInfo.specialMethods); err != nil {
			return nil, err
		}
		if err := writeStringMap(buf, classInfo.methodVisibility); err != nil {
			return nil, err
		}
		if err := writeStringMap(buf, classInfo.staticVisibility); err != nil {
			return nil, err
		}
		if err := writeAnnotationMap(buf, classInfo.methodAnnotations); err != nil {
			return nil, err
		}
		if err := writeUTF8(buf, classInfo.constructorVisibility); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func writeEncodedFieldDefs(w io.Writer, defs []encodedFieldDef) error {
	if err := writeU2(w, uint16(len(defs))); err != nil {
		return err
	}
	for _, def := range defs {
		if err := writeUTF8(w, def.name); err != nil {
			return err
		}
		if err := writeUTF8(w, def.typeName); err != nil {
			return err
		}
		if err := writeUTF8(w, def.visibility); err != nil {
			return err
		}
		var mutable byte
		if def.mutable {
			mutable = 1
		}
		if err := writeU1(w, mutable); err != nil {
			return err
		}
	}
	return nil
}

func writeMethodBindings(w io.Writer, bindings []encodedMethodBinding) error {
	if err := writeU2(w, uint16(len(bindings))); err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := writeUTF8(w, binding.name); err != nil {
			return err
		}
		if err := writeU2(w, binding.methodIndex); err != nil {
			return err
		}
	}
	return nil
}

func writeOverloads(w io.Writer, overloads map[string][]uint16) error {
	if err := writeU2(w, uint16(len(overloads))); err != nil {
		return err
	}
	names := make([]string, 0, len(overloads))
	for name := range overloads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := writeUTF8(w, name); err != nil {
			return err
		}
		if err := writeU2List(w, overloads[name]); err != nil {
			return err
		}
	}
	return nil
}

func writeSpecialMethods(w io.Writer, methods []encodedSpecialMethod) error {
	if err := writeU2(w, uint16(len(methods))); err != nil {
		return err
	}
	for _, method := range methods {
		if err := writeU1(w, method.slot); err != nil {
			return err
		}
		if err := writeU2(w, method.methodIndex); err != nil {
			return err
		}
	}
	return nil
}

func writeStringMap(w io.Writer, src map[string]string) error {
	if err := writeU2(w, uint16(len(src))); err != nil {
		return err
	}
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writeUTF8(w, key); err != nil {
			return err
		}
		if err := writeUTF8(w, src[key]); err != nil {
			return err
		}
	}
	return nil
}

func writeAnnotationMap(w io.Writer, src map[string][]string) error {
	if err := writeU2(w, uint16(len(src))); err != nil {
		return err
	}
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writeUTF8(w, key); err != nil {
			return err
		}
		if err := writeStringList(w, src[key]); err != nil {
			return err
		}
	}
	return nil
}

func writeU2List(w io.Writer, values []uint16) error {
	if err := writeU2(w, uint16(len(values))); err != nil {
		return err
	}
	for _, value := range values {
		if err := writeU2(w, value); err != nil {
			return err
		}
	}
	return nil
}

func writeStringList(w io.Writer, values []string) error {
	if err := writeU2(w, uint16(len(values))); err != nil {
		return err
	}
	for _, value := range values {
		if err := writeUTF8(w, value); err != nil {
			return err
		}
	}
	return nil
}

func writeAttribute(w io.Writer, attr attributeWire) error {
	if err := writeU2(w, attr.nameIndex); err != nil {
		return err
	}
	if err := writeU4(w, uint32(len(attr.info))); err != nil {
		return err
	}
	_, err := w.Write(attr.info)
	return err
}

func readModuleClassFile(r io.Reader) (*bytecode.Function, []byte, error) {
	magic, err := readU4(r)
	if err != nil {
		return nil, nil, err
	}
	if magic != Magic {
		return nil, nil, fmt.Errorf("invalid classfile magic 0x%08X", magic)
	}
	minor, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	major, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	if major != MajorVersion || minor != MinorVersion {
		return nil, nil, fmt.Errorf("unsupported classfile version %d.%d", major, minor)
	}
	cp, err := readCP(r)
	if err != nil {
		return nil, nil, err
	}
	if _, err := readU2(r); err != nil {
		return nil, nil, err
	}
	if _, err := readU2(r); err != nil {
		return nil, nil, err
	}
	if _, err := readU2(r); err != nil {
		return nil, nil, err
	}
	interfacesCount, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < int(interfacesCount); i++ {
		if _, err := readU2(r); err != nil {
			return nil, nil, err
		}
	}
	fieldsCount, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < int(fieldsCount); i++ {
		if err := skipMember(r); err != nil {
			return nil, nil, err
		}
	}
	methodsCount, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	methods := make([]decodedMethod, methodsCount)
	for i := 0; i < int(methodsCount); i++ {
		method, err := readMethod(r, cp)
		if err != nil {
			return nil, nil, err
		}
		methods[i] = method
	}
	attributesCount, err := readU2(r)
	if err != nil {
		return nil, nil, err
	}
	var entryMethod uint16
	var classes []encodedClass
	var metadata []byte
	for i := 0; i < int(attributesCount); i++ {
		name, info, err := readAttribute(r, cp)
		if err != nil {
			return nil, nil, err
		}
		switch name {
		case "PolyloftModule":
			entryMethod, err = decodeModuleAttribute(info)
			if err != nil {
				return nil, nil, err
			}
		case "PolyloftClasses":
			classes, err = decodeClassesAttribute(info)
			if err != nil {
				return nil, nil, err
			}
		case "PolyloftMetadata":
			metadata = info
		}
	}
	functionTable := make(map[uint16]*bytecode.Function, len(methods))
	for i, method := range methods {
		functionTable[uint16(i+1)] = &bytecode.Function{Name: method.name}
	}
	classTable := make(map[uint16]*value.Class, len(classes))
	for _, classInfo := range classes {
		classTable[classInfo.id] = &value.Class{}
	}
	for _, classInfo := range classes {
		populateClass(classTable[classInfo.id], classInfo, classTable, functionTable)
	}
	for i, method := range methods {
		populateFunction(functionTable[uint16(i+1)], method, cp, functionTable, classTable)
	}
	entry := functionTable[entryMethod]
	if entry == nil {
		return nil, nil, fmt.Errorf("missing entry method %d", entryMethod)
	}
	return entry, metadata, nil
}

type decodedMethod struct {
	name            string
	descriptor      string
	accessFlags     uint16
	code            []byte
	lines           []int
	ownerClassName  string
	returnType      string
	upvalues        []bytecode.UpvalueSpec
	globalSlotCount uint16
	globalSlotNames []string
	interfaces      map[string]bytecode.InterfaceSpec
	constants       []encodedConstant
	maxLocals       uint16
}

func skipMember(r io.Reader) error {
	if _, err := readU2(r); err != nil {
		return err
	}
	if _, err := readU2(r); err != nil {
		return err
	}
	if _, err := readU2(r); err != nil {
		return err
	}
	attributesCount, err := readU2(r)
	if err != nil {
		return err
	}
	for i := 0; i < int(attributesCount); i++ {
		if _, err := readU2(r); err != nil {
			return err
		}
		length, err := readU4(r)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
			return err
		}
	}
	return nil
}

func readMethod(r io.Reader, cp *cpReader) (decodedMethod, error) {
	_, err := readU2(r)
	if err != nil {
		return decodedMethod{}, err
	}
	nameIndex, err := readU2(r)
	if err != nil {
		return decodedMethod{}, err
	}
	descriptorIndex, err := readU2(r)
	if err != nil {
		return decodedMethod{}, err
	}
	attributesCount, err := readU2(r)
	if err != nil {
		return decodedMethod{}, err
	}
	name, err := cp.UTF8(nameIndex)
	if err != nil {
		return decodedMethod{}, err
	}
	descriptor, err := cp.UTF8(descriptorIndex)
	if err != nil {
		return decodedMethod{}, err
	}
	method := decodedMethod{name: name, descriptor: descriptor}
	for i := 0; i < int(attributesCount); i++ {
		attrName, info, err := readAttribute(r, cp)
		if err != nil {
			return decodedMethod{}, err
		}
		switch attrName {
		case "Code":
			method.code, method.lines, method.maxLocals, err = decodeCodeAttribute(info, cp)
			if err != nil {
				return decodedMethod{}, err
			}
		case "PolyloftMethod":
			ownerClassName, returnType, upvalues, globalSlotCount, globalSlotNames, interfaces, constants, err := decodeMethodMeta(info)
			if err != nil {
				return decodedMethod{}, err
			}
			method.ownerClassName = ownerClassName
			method.returnType = returnType
			method.upvalues = upvalues
			method.globalSlotCount = globalSlotCount
			method.globalSlotNames = globalSlotNames
			method.interfaces = interfaces
			method.constants = constants
		}
	}
	return method, nil
}

func readAttribute(r io.Reader, cp *cpReader) (string, []byte, error) {
	nameIndex, err := readU2(r)
	if err != nil {
		return "", nil, err
	}
	length, err := readU4(r)
	if err != nil {
		return "", nil, err
	}
	name, err := cp.UTF8(nameIndex)
	if err != nil {
		return "", nil, err
	}
	info := make([]byte, length)
	if _, err := io.ReadFull(r, info); err != nil {
		return "", nil, err
	}
	return name, info, nil
}

func decodeModuleAttribute(info []byte) (uint16, error) {
	reader := bytes.NewReader(info)
	return readU2(reader)
}

func decodeCodeAttribute(info []byte, cp *cpReader) ([]byte, []int, uint16, error) {
	reader := bytes.NewReader(info)
	if _, err := readU2(reader); err != nil {
		return nil, nil, 0, err
	}
	maxLocals, err := readU2(reader)
	if err != nil {
		return nil, nil, 0, err
	}
	codeLength, err := readU4(reader)
	if err != nil {
		return nil, nil, 0, err
	}
	code := make([]byte, codeLength)
	if _, err := io.ReadFull(reader, code); err != nil {
		return nil, nil, 0, err
	}
	exceptionTableLength, err := readU2(reader)
	if err != nil {
		return nil, nil, 0, err
	}
	if _, err := io.CopyN(io.Discard, reader, int64(exceptionTableLength)*8); err != nil {
		return nil, nil, 0, err
	}
	attrCount, err := readU2(reader)
	if err != nil {
		return nil, nil, 0, err
	}
	var lines []int
	for i := 0; i < int(attrCount); i++ {
		name, nestedInfo, err := readAttribute(reader, cp)
		if err != nil {
			return nil, nil, 0, err
		}
		if name == "LineNumberTable" {
			lines, err = decodeLineNumberTable(nestedInfo, int(codeLength))
			if err != nil {
				return nil, nil, 0, err
			}
		}
	}
	if lines == nil {
		lines = make([]int, len(code))
	}
	return code, lines, maxLocals, nil
}

func decodeLineNumberTable(info []byte, codeLength int) ([]int, error) {
	reader := bytes.NewReader(info)
	count, err := readU2(reader)
	if err != nil {
		return nil, err
	}
	entries := make([]lineEntry, 0, count)
	for i := 0; i < int(count); i++ {
		offset, err := readU2(reader)
		if err != nil {
			return nil, err
		}
		line, err := readU2(reader)
		if err != nil {
			return nil, err
		}
		entries = append(entries, lineEntry{offset: int(offset), line: int(line)})
	}
	lines := make([]int, codeLength)
	currentLine := 0
	entryIndex := 0
	for offset := 0; offset < codeLength; offset++ {
		for entryIndex < len(entries) && entries[entryIndex].offset == offset {
			currentLine = entries[entryIndex].line
			entryIndex++
		}
		lines[offset] = currentLine
	}
	return lines, nil
}

func decodeMethodMeta(info []byte) (string, string, []bytecode.UpvalueSpec, uint16, []string, map[string]bytecode.InterfaceSpec, []encodedConstant, error) {
	reader := bytes.NewReader(info)
	ownerClassName, err := readUTF8(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	returnType, err := readUTF8(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	upvalueCount, err := readU2(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	upvalues := make([]bytecode.UpvalueSpec, upvalueCount)
	for i := 0; i < int(upvalueCount); i++ {
		index, err := readU1(reader)
		if err != nil {
			return "", "", nil, 0, nil, nil, nil, err
		}
		isLocal, err := readU1(reader)
		if err != nil {
			return "", "", nil, 0, nil, nil, nil, err
		}
		upvalues[i] = bytecode.UpvalueSpec{Index: index, IsLocal: isLocal == 1}
	}
	globalSlotCount, err := readU2(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	globalSlotNames, err := readStringList(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	interfaces, err := decodeInterfaces(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	constantCount, err := readU2(reader)
	if err != nil {
		return "", "", nil, 0, nil, nil, nil, err
	}
	constants := make([]encodedConstant, constantCount)
	for i := 0; i < int(constantCount); i++ {
		kind, err := readU1(reader)
		if err != nil {
			return "", "", nil, 0, nil, nil, nil, err
		}
		cpIndex, err := readU2(reader)
		if err != nil {
			return "", "", nil, 0, nil, nil, nil, err
		}
		refIndex, err := readU2(reader)
		if err != nil {
			return "", "", nil, 0, nil, nil, nil, err
		}
		constants[i] = encodedConstant{kind: kind, cpIndex: cpIndex, refIndex: refIndex}
	}
	return ownerClassName, returnType, upvalues, globalSlotCount, globalSlotNames, interfaces, constants, nil
}

func decodeInterfaces(r io.Reader) (map[string]bytecode.InterfaceSpec, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	interfaces := make(map[string]bytecode.InterfaceSpec, count)
	for i := 0; i < int(count); i++ {
		key, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		name, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		functionalMethod, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		methodCount, err := readU2(r)
		if err != nil {
			return nil, err
		}
		methods := make(map[string]bytecode.InterfaceMethodSpec, methodCount)
		for j := 0; j < int(methodCount); j++ {
			methodKey, err := readUTF8(r)
			if err != nil {
				return nil, err
			}
			methodName, err := readUTF8(r)
			if err != nil {
				return nil, err
			}
			params, err := readStringList(r)
			if err != nil {
				return nil, err
			}
			ret, err := readUTF8(r)
			if err != nil {
				return nil, err
			}
			methods[methodKey] = bytecode.InterfaceMethodSpec{Name: methodName, Params: params, Return: ret}
		}
		interfaces[key] = bytecode.InterfaceSpec{Name: name, Methods: methods, FunctionalMethod: functionalMethod}
	}
	return interfaces, nil
}

func decodeClassesAttribute(info []byte) ([]encodedClass, error) {
	reader := bytes.NewReader(info)
	count, err := readU2(reader)
	if err != nil {
		return nil, err
	}
	classes := make([]encodedClass, count)
	for i := 0; i < int(count); i++ {
		classInfo, err := decodeClass(reader)
		if err != nil {
			return nil, err
		}
		classes[i] = classInfo
	}
	return classes, nil
}

func decodeClass(r io.Reader) (encodedClass, error) {
	id, err := readU2(r)
	if err != nil {
		return encodedClass{}, err
	}
	name, err := readUTF8(r)
	if err != nil {
		return encodedClass{}, err
	}
	superclassID, err := readU2(r)
	if err != nil {
		return encodedClass{}, err
	}
	superclassName, err := readUTF8(r)
	if err != nil {
		return encodedClass{}, err
	}
	flags, err := readU2(r)
	if err != nil {
		return encodedClass{}, err
	}
	implements, err := readStringList(r)
	if err != nil {
		return encodedClass{}, err
	}
	permits, err := readStringList(r)
	if err != nil {
		return encodedClass{}, err
	}
	enumOrder, err := readStringList(r)
	if err != nil {
		return encodedClass{}, err
	}
	fields, err := readEncodedFieldDefs(r)
	if err != nil {
		return encodedClass{}, err
	}
	fieldOrder, err := readStringList(r)
	if err != nil {
		return encodedClass{}, err
	}
	fieldSlots, err := readMethodBindings(r)
	if err != nil {
		return encodedClass{}, err
	}
	staticFields, err := readEncodedFieldDefs(r)
	if err != nil {
		return encodedClass{}, err
	}
	methodOrder, err := readStringList(r)
	if err != nil {
		return encodedClass{}, err
	}
	methodSlots, err := readMethodBindings(r)
	if err != nil {
		return encodedClass{}, err
	}
	methodOverloads, err := readOverloads(r)
	if err != nil {
		return encodedClass{}, err
	}
	constructor, err := readU2(r)
	if err != nil {
		return encodedClass{}, err
	}
	constructorOverloads, err := readU2List(r)
	if err != nil {
		return encodedClass{}, err
	}
	staticMethods, err := readMethodBindings(r)
	if err != nil {
		return encodedClass{}, err
	}
	staticMethodOverloads, err := readOverloads(r)
	if err != nil {
		return encodedClass{}, err
	}
	specialMethods, err := readSpecialMethods(r)
	if err != nil {
		return encodedClass{}, err
	}
	methodVisibility, err := readStringMap(r)
	if err != nil {
		return encodedClass{}, err
	}
	staticVisibility, err := readStringMap(r)
	if err != nil {
		return encodedClass{}, err
	}
	methodAnnotations, err := readAnnotationMap(r)
	if err != nil {
		return encodedClass{}, err
	}
	constructorVisibility, err := readUTF8(r)
	if err != nil {
		return encodedClass{}, err
	}
	return encodedClass{
		id: id, name: name, superclassID: superclassID, superclassName: superclassName, flags: flags,
		implements: implements, permits: permits, enumOrder: enumOrder, fields: fields, fieldOrder: fieldOrder,
		fieldSlots: fieldSlots, staticFields: staticFields, methodOrder: methodOrder, methodSlots: methodSlots,
		methodOverloads: methodOverloads, constructor: constructor, constructorOverloads: constructorOverloads,
		staticMethods: staticMethods, staticMethodOverloads: staticMethodOverloads, specialMethods: specialMethods,
		methodVisibility: methodVisibility, staticVisibility: staticVisibility, methodAnnotations: methodAnnotations,
		constructorVisibility: constructorVisibility,
	}, nil
}

func readEncodedFieldDefs(r io.Reader) ([]encodedFieldDef, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	defs := make([]encodedFieldDef, count)
	for i := 0; i < int(count); i++ {
		name, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		typeName, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		visibility, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		mutable, err := readU1(r)
		if err != nil {
			return nil, err
		}
		defs[i] = encodedFieldDef{name: name, typeName: typeName, visibility: visibility, mutable: mutable == 1}
	}
	return defs, nil
}

func readMethodBindings(r io.Reader) ([]encodedMethodBinding, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	bindings := make([]encodedMethodBinding, count)
	for i := 0; i < int(count); i++ {
		name, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		methodIndex, err := readU2(r)
		if err != nil {
			return nil, err
		}
		bindings[i] = encodedMethodBinding{name: name, methodIndex: methodIndex}
	}
	return bindings, nil
}

func readOverloads(r io.Reader) (map[string][]uint16, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	overloads := make(map[string][]uint16, count)
	for i := 0; i < int(count); i++ {
		name, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		values, err := readU2List(r)
		if err != nil {
			return nil, err
		}
		overloads[name] = values
	}
	return overloads, nil
}

func readSpecialMethods(r io.Reader) ([]encodedSpecialMethod, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	methods := make([]encodedSpecialMethod, count)
	for i := 0; i < int(count); i++ {
		slot, err := readU1(r)
		if err != nil {
			return nil, err
		}
		methodIndex, err := readU2(r)
		if err != nil {
			return nil, err
		}
		methods[i] = encodedSpecialMethod{slot: slot, methodIndex: methodIndex}
	}
	return methods, nil
}

func readStringMap(r io.Reader) (map[string]string, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, count)
	for i := 0; i < int(count); i++ {
		key, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		value, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

func readAnnotationMap(r io.Reader) (map[string][]string, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, count)
	for i := 0; i < int(count); i++ {
		key, err := readUTF8(r)
		if err != nil {
			return nil, err
		}
		values, err := readStringList(r)
		if err != nil {
			return nil, err
		}
		result[key] = values
	}
	return result, nil
}

func readU2List(r io.Reader) ([]uint16, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	values := make([]uint16, count)
	for i := 0; i < int(count); i++ {
		values[i], err = readU2(r)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func readStringList(r io.Reader) ([]string, error) {
	count, err := readU2(r)
	if err != nil {
		return nil, err
	}
	values := make([]string, count)
	for i := 0; i < int(count); i++ {
		values[i], err = readUTF8(r)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func populateClass(classValue *value.Class, classInfo encodedClass, classTable map[uint16]*value.Class, functionTable map[uint16]*bytecode.Function) {
	classValue.ClassDecl = value.ClassDecl{
		Name:                  classInfo.name,
		Superclass:            classTable[classInfo.superclassID],
		Implements:            toBoolMap(classInfo.implements),
		Permits:               toBoolMap(classInfo.permits),
		IsAbstract:            false,
		IsEnum:                classInfo.flags&accFinal != 0 && len(classInfo.enumOrder) > 0,
		IsSealed:              classInfo.flags&accFinal != 0,
		IsRecord:              false,
		EnumOrder:             append([]string(nil), classInfo.enumOrder...),
		Fields:                decodeFieldDefs(classInfo.fields),
		FieldOrder:            append([]string(nil), classInfo.fieldOrder...),
		FieldIndex:            decodeFieldSlots(classInfo.fieldSlots),
		StaticFields:          decodeFieldDefs(classInfo.staticFields),
		MethodVisibility:      cloneStringMap(classInfo.methodVisibility),
		StaticVisibility:      cloneStringMap(classInfo.staticVisibility),
		ConstructorVisibility: classInfo.constructorVisibility,
		MethodAnnotations:     cloneAnnotations(classInfo.methodAnnotations),
	}
	classValue.ClassRuntime = value.ClassRuntime{
		MethodOrder:           append([]string(nil), classInfo.methodOrder...),
		MethodIndex:           make(map[string]int),
		MethodTable:           make([]*bytecode.Function, len(classInfo.methodOrder)),
		Methods:               make(map[string]*bytecode.Function),
		MethodOverloads:       decodeOverloadsToFunctions(classInfo.methodOverloads, functionTable),
		Constructor:           functionTable[classInfo.constructor],
		ConstructorOverloads:  decodeOverloadSliceToFunctions(classInfo.constructorOverloads, functionTable),
		StaticMethods:         make(map[string]*bytecode.Function),
		StaticMethodOverloads: decodeOverloadsToFunctions(classInfo.staticMethodOverloads, functionTable),
		StaticValues:          make(map[string]value.Value),
		SpecialMethods:        make(map[value.SpecialMethodSlot]*bytecode.Function),
	}
	for index, name := range classInfo.methodOrder {
		classValue.MethodIndex[name] = index
	}
	for _, binding := range classInfo.methodSlots {
		fn := functionTable[binding.methodIndex]
		classValue.Methods[binding.name] = fn
		if slot, ok := classValue.MethodIndex[binding.name]; ok && slot < len(classValue.MethodTable) {
			classValue.MethodTable[slot] = fn
		}
	}
	for _, binding := range classInfo.staticMethods {
		classValue.StaticMethods[binding.name] = functionTable[binding.methodIndex]
	}
	for _, special := range classInfo.specialMethods {
		classValue.SpecialMethods[value.SpecialMethodSlot(special.slot)] = functionTable[special.methodIndex]
	}
}

func populateFunction(fn *bytecode.Function, method decodedMethod, cp *cpReader, functionTable map[uint16]*bytecode.Function, classTable map[uint16]*value.Class) {
	params, ret := parseMethodDescriptor(method.descriptor)
	constants := make([]any, len(method.constants))
	for i, constant := range method.constants {
		constants[i] = decodeConstant(cp, constant, functionTable, classTable)
	}
	fn.Name = method.name
	fn.ParamTypes = params
	fn.Arity = len(params)
	fn.OwnerClassName = method.ownerClassName
	fn.Upvalues = append([]bytecode.UpvalueSpec(nil), method.upvalues...)
	fn.ReturnType = ret
	if method.returnType != "" {
		fn.ReturnType = method.returnType
	}
	fn.MaxLocals = int(method.maxLocals)
	fn.GlobalSlotCount = int(method.globalSlotCount)
	fn.GlobalSlotNames = append([]string(nil), method.globalSlotNames...)
	fn.Interfaces = cloneInterfaces(method.interfaces)
	fn.Chunk = &bytecode.Chunk{Code: append([]byte(nil), method.code...), Lines: append([]int(nil), method.lines...), Constants: constants}
}

func toBoolMap(values []string) map[string]bool {
	if len(values) == 0 {
		return make(map[string]bool)
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func decodeFieldDefs(defs []encodedFieldDef) map[string]value.FieldDef {
	result := make(map[string]value.FieldDef, len(defs))
	for _, def := range defs {
		result[def.name] = value.FieldDef{Mutable: def.mutable, IsFinal: def.isFinal, TypeName: def.typeName, Visibility: def.visibility}
	}
	return result
}

func decodeFieldSlots(bindings []encodedMethodBinding) map[string]int {
	result := make(map[string]int, len(bindings))
	for _, binding := range bindings {
		result[binding.name] = int(binding.methodIndex)
	}
	return result
}

func decodeOverloadsToFunctions(src map[string][]uint16, table map[uint16]*bytecode.Function) map[string][]*bytecode.Function {
	result := make(map[string][]*bytecode.Function, len(src))
	for name, overloads := range src {
		result[name] = decodeOverloadSliceToFunctions(overloads, table)
	}
	return result
}

func decodeOverloadSliceToFunctions(src []uint16, table map[uint16]*bytecode.Function) []*bytecode.Function {
	result := make([]*bytecode.Function, 0, len(src))
	for _, index := range src {
		if fn := table[index]; fn != nil {
			result = append(result, fn)
		}
	}
	return result
}

func decodeConstant(cp *cpReader, constant encodedConstant, functionTable map[uint16]*bytecode.Function, classTable map[uint16]*value.Class) any {
	switch constant.kind {
	case constNil:
		return nil
	case constInt64:
		value, err := cp.Int64(constant.cpIndex)
		if err == nil {
			return value
		}
	case constFloat64:
		value, err := cp.Float64(constant.cpIndex)
		if err == nil {
			return value
		}
	case constString:
		value, err := cp.String(constant.cpIndex)
		if err == nil {
			return value
		}
	case constRune:
		value, err := cp.Int32(constant.cpIndex)
		if err == nil {
			return rune(value)
		}
	case constBool:
		value, err := cp.Int32(constant.cpIndex)
		if err == nil {
			return value != 0
		}
	case constFunctionRef:
		return functionTable[constant.refIndex]
	case constClassRef:
		return classTable[constant.refIndex]
	}
	return nil
}

func parseMethodDescriptor(descriptor string) ([]string, string) {
	if descriptor == "" || descriptor[0] != '(' {
		return nil, ""
	}
	params := make([]string, 0)
	index := 1
	for index < len(descriptor) && descriptor[index] != ')' {
		name, next := parseTypeDescriptor(descriptor, index)
		params = append(params, name)
		index = next
	}
	if index >= len(descriptor) {
		return params, ""
	}
	ret, _ := parseTypeDescriptor(descriptor, index+1)
	return params, ret
}

func parseTypeDescriptor(descriptor string, index int) (string, int) {
	if index >= len(descriptor) {
		return "", index
	}
	switch descriptor[index] {
	case 'V':
		return "Void", index + 1
	case 'I':
		return "Int", index + 1
	case 'D':
		return "Float", index + 1
	case 'Z':
		return "Bool", index + 1
	case 'C':
		return "Char", index + 1
	case '[':
		inner, next := parseTypeDescriptor(descriptor, index+1)
		return inner + "[]", next
	case 'L':
		end := strings.IndexByte(descriptor[index:], ';')
		if end < 0 {
			return "Any", len(descriptor)
		}
		name := descriptor[index+1 : index+end]
		switch name {
		case "java/lang/String":
			return "String", index + end + 1
		case "polyloft/Any":
			return "Any", index + end + 1
		case "polyloft/Function":
			return "Function", index + end + 1
		case "polyloft/Array":
			return "Array", index + end + 1
		case "polyloft/Map":
			return "Map", index + end + 1
		case "polyloft/Tuple":
			return "Tuple", index + end + 1
		case "polyloft/Range":
			return "Range", index + end + 1
		case "polyloft/Module":
			return "Module", index + end + 1
		case "polyloft/Nil":
			return "Nil", index + end + 1
		default:
			return strings.ReplaceAll(name, "/", "."), index + end + 1
		}
	default:
		return "Any", index + 1
	}
}

func writeU1(w io.Writer, value byte) error   { return binary.Write(w, binary.BigEndian, value) }
func writeU2(w io.Writer, value uint16) error { return binary.Write(w, binary.BigEndian, value) }
func writeU4(w io.Writer, value uint32) error { return binary.Write(w, binary.BigEndian, value) }

func readU1(r io.Reader) (byte, error) {
	var value byte
	err := binary.Read(r, binary.BigEndian, &value)
	return value, err
}
func readU2(r io.Reader) (uint16, error) {
	var value uint16
	err := binary.Read(r, binary.BigEndian, &value)
	return value, err
}
func readU4(r io.Reader) (uint32, error) {
	var value uint32
	err := binary.Read(r, binary.BigEndian, &value)
	return value, err
}

func writeUTF8(w io.Writer, value string) error {
	data := []byte(value)
	if len(data) > math.MaxUint16 {
		return fmt.Errorf("utf8 constant too large: %d", len(data))
	}
	if err := writeU2(w, uint16(len(data))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readUTF8(r io.Reader) (string, error) {
	length, err := readU2(r)
	if err != nil {
		return "", err
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}
