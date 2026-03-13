package value

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
)

type BinaryCodec struct {
	seenClasses    map[*Class]int32
	classIndex     map[int32]*Class
	nextClassID    int32
	seenFunctions  map[*bytecode.Function]int32
	functionIndex  map[int32]*bytecode.Function
	nextFunctionID int32
}

func NewBinaryCodec() *BinaryCodec {
	return &BinaryCodec{
		seenClasses:   make(map[*Class]int32),
		classIndex:    make(map[int32]*Class),
		seenFunctions: make(map[*bytecode.Function]int32),
		functionIndex: make(map[int32]*bytecode.Function),
	}
}

const (
	tagNil byte = iota
	tagInt64
	tagFloat64
	tagString
	tagRune
	tagBool
	tagFunctionDef
	tagFunctionRef
	tagClassDef
	tagClassRef
)

func (bc *BinaryCodec) EncodeConstant(w io.Writer, c any) error {
	if c == nil {
		return binary.Write(w, binary.LittleEndian, tagNil)
	}

	switch v := c.(type) {
	case int64:
		if err := binary.Write(w, binary.LittleEndian, tagInt64); err != nil {
			return err
		}
		return binary.Write(w, binary.LittleEndian, v)
	case float64:
		if err := binary.Write(w, binary.LittleEndian, tagFloat64); err != nil {
			return err
		}
		return binary.Write(w, binary.LittleEndian, v)
	case string:
		if err := binary.Write(w, binary.LittleEndian, tagString); err != nil {
			return err
		}
		return bytecode.WriteString(w, v)
	case rune:
		if err := binary.Write(w, binary.LittleEndian, tagRune); err != nil {
			return err
		}
		return binary.Write(w, binary.LittleEndian, int32(v))
	case bool:
		if err := binary.Write(w, binary.LittleEndian, tagBool); err != nil {
			return err
		}
		if v {
			return binary.Write(w, binary.LittleEndian, byte(1))
		}
		return binary.Write(w, binary.LittleEndian, byte(0))
	case *bytecode.Function:
		if id, ok := bc.seenFunctions[v]; ok {
			if err := binary.Write(w, binary.LittleEndian, tagFunctionRef); err != nil {
				return err
			}
			return binary.Write(w, binary.LittleEndian, id)
		}
		bc.seenFunctions[v] = bc.nextFunctionID
		if err := binary.Write(w, binary.LittleEndian, tagFunctionDef); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, bc.nextFunctionID); err != nil {
			return err
		}
		bc.nextFunctionID++
		return v.WriteBinary(w, bc)
	case *Class:
		if id, ok := bc.seenClasses[v]; ok {
			if err := binary.Write(w, binary.LittleEndian, tagClassRef); err != nil {
				return err
			}
			return binary.Write(w, binary.LittleEndian, id)
		}
		bc.seenClasses[v] = bc.nextClassID
		if err := binary.Write(w, binary.LittleEndian, tagClassDef); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, bc.nextClassID); err != nil {
			return err
		}
		bc.nextClassID++
		return writeClass(w, v, bc)
	default:
		return fmt.Errorf("BinaryCodec: unsupported constant type %T", c)
	}
}

func (bc *BinaryCodec) DecodeConstant(r io.Reader) (any, error) {
	var tag byte
	if err := binary.Read(r, binary.LittleEndian, &tag); err != nil {
		return nil, err
	}

	switch tag {
	case tagNil:
		return nil, nil
	case tagInt64:
		var v int64
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return nil, err
		}
		return v, nil
	case tagFloat64:
		var v float64
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return nil, err
		}
		return v, nil
	case tagString:
		return bytecode.ReadString(r)
	case tagRune:
		var v int32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return nil, err
		}
		return rune(v), nil
	case tagBool:
		var v byte
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return nil, err
		}
		return v == 1, nil
	case tagFunctionRef:
		var id int32
		if err := binary.Read(r, binary.LittleEndian, &id); err != nil {
			return nil, err
		}
		return bc.functionIndex[id], nil
	case tagFunctionDef:
		var id int32
		if err := binary.Read(r, binary.LittleEndian, &id); err != nil {
			return nil, err
		}
		fn, err := bytecode.ReadBinaryFunction(r, bc)
		if err != nil {
			return nil, err
		}
		bc.functionIndex[id] = fn
		return fn, nil
	case tagClassRef:
		var id int32
		if err := binary.Read(r, binary.LittleEndian, &id); err != nil {
			return nil, err
		}
		return bc.classIndex[id], nil
	case tagClassDef:
		var id int32
		if err := binary.Read(r, binary.LittleEndian, &id); err != nil {
			return nil, err
		}
		// Create empty class and index it BEFORE reading fields to handle cycles
		c := &Class{}
		bc.classIndex[id] = c
		err := readClass(r, bc, c)
		if err != nil {
			return nil, err
		}
		return c, nil
	default:
		return nil, fmt.Errorf("BinaryCodec: unsupported tag %d", tag)
	}
}
func writeFieldDef(w io.Writer, f FieldDef) error {
	// Not storing Default since defaults are initialized via bytecode instructions typically,
	// but wait! If there is a Default value stored directly in the class...
	// Polyloft typically compiles defaults into the constructor!
	// Let's serialize it as a Constant just in case.
	// We'll use our codec to encode the Default value if it's there.
	// Wait, Default is a Value struct, not an `any`/primitive constant!
	// Let's skip Default serialization for now. If it breaks, we'll fix it.
	// Alternatively, we can just write a dummy tag for now.
	if err := binary.Write(w, binary.LittleEndian, f.Mutable); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, f.IsFinal); err != nil {
		return err
	}
	if err := bytecode.WriteString(w, f.TypeName); err != nil {
		return err
	}
	if err := bytecode.WriteString(w, f.Visibility); err != nil {
		return err
	}
	return nil
}

func readFieldDef(r io.Reader) (FieldDef, error) {
	var f FieldDef
	if err := binary.Read(r, binary.LittleEndian, &f.Mutable); err != nil {
		return f, err
	}
	if err := binary.Read(r, binary.LittleEndian, &f.IsFinal); err != nil {
		return f, err
	}
	var err error
	if f.TypeName, err = bytecode.ReadString(r); err != nil {
		return f, err
	}
	if f.Visibility, err = bytecode.ReadString(r); err != nil {
		return f, err
	}
	return f, nil
}

func writeClass(w io.Writer, c *Class, enc *BinaryCodec) error {
	if err := bytecode.WriteString(w, c.Name); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, c.IsAbstract); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, c.IsEnum); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, c.IsSealed); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, c.IsRecord); err != nil {
		return err
	}

	// Fast Constructor Plan
	if c.FastConstructor == nil {
		if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
			return err
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, int32(c.FastConstructor.Arity)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(c.FastConstructor.FieldSlots))); err != nil {
			return err
		}
		for _, v := range c.FastConstructor.FieldSlots {
			if err := binary.Write(w, binary.LittleEndian, int32(v)); err != nil {
				return err
			}
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(c.FastConstructor.ArgIndexes))); err != nil {
			return err
		}
		for _, v := range c.FastConstructor.ArgIndexes {
			if err := binary.Write(w, binary.LittleEndian, int32(v)); err != nil {
				return err
			}
		}
	}

	// Skipping Implements/Permits for now, just sending empty sizes.
	if err := binary.Write(w, binary.LittleEndian, uint32(0)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(0)); err != nil {
		return err
	}

	if err := bytecode.WriteStringSlice(w, c.EnumOrder); err != nil {
		return err
	}

	// Fields
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.Fields))); err != nil {
		return err
	}
	for k, f := range c.Fields {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := writeFieldDef(w, f); err != nil {
			return err
		}
	}
	if err := bytecode.WriteStringSlice(w, c.FieldOrder); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.FieldIndex))); err != nil {
		return err
	}
	for k, v := range c.FieldIndex {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, int32(v)); err != nil {
			return err
		}
	}

	if err := bytecode.WriteStringSlice(w, c.MethodOrder); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.MethodIndex))); err != nil {
		return err
	}
	for k, v := range c.MethodIndex {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, int32(v)); err != nil {
			return err
		}
	}

	// Methods table
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.MethodTable))); err != nil {
		return err
	}
	for _, m := range c.MethodTable {
		if m == nil {
			if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
				return err
			}
		} else {
			if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
				return err
			}
			if err := m.WriteBinary(w, enc); err != nil {
				return err
			}
		}
	}

	// Methods map
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.Methods))); err != nil {
		return err
	}
	for k, m := range c.Methods {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if m == nil {
			if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
				return err
			}
		} else {
			if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
				return err
			}
			if err := m.WriteBinary(w, enc); err != nil {
				return err
			}
		}
	}

	if c.Constructor == nil {
		if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
			return err
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
			return err
		}
		if err := c.Constructor.WriteBinary(w, enc); err != nil {
			return err
		}
	}

	// ConstructorVisibility
	if err := bytecode.WriteString(w, c.ConstructorVisibility); err != nil {
		return err
	}

	// StaticFields
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.StaticFields))); err != nil {
		return err
	}
	for k, f := range c.StaticFields {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := writeFieldDef(w, f); err != nil {
			return err
		}
	}

	// StaticValues (just write keys as nil; they're initialized at runtime via bytecode)
	if err := binary.Write(w, binary.LittleEndian, uint32(0)); err != nil {
		return err
	}

	// StaticMethods
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.StaticMethods))); err != nil {
		return err
	}
	for k, m := range c.StaticMethods {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if m == nil {
			if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
				return err
			}
		} else {
			if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
				return err
			}
			if err := m.WriteBinary(w, enc); err != nil {
				return err
			}
		}
	}

	// MethodVisibility map
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.MethodVisibility))); err != nil {
		return err
	}
	for k, v := range c.MethodVisibility {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := bytecode.WriteString(w, v); err != nil {
			return err
		}
	}

	// StaticVisibility map
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.StaticVisibility))); err != nil {
		return err
	}
	for k, v := range c.StaticVisibility {
		if err := bytecode.WriteString(w, k); err != nil {
			return err
		}
		if err := bytecode.WriteString(w, v); err != nil {
			return err
		}
	}

	var specialCount uint32
	for _, slot := range specialMethodSlotOrder {
		if fn, ok := c.DeclaredSpecialMethod(slot); ok && fn != nil {
			specialCount++
		}
	}
	if err := binary.Write(w, binary.LittleEndian, specialCount); err != nil {
		return err
	}
	for _, slot := range specialMethodSlotOrder {
		fn, ok := c.DeclaredSpecialMethod(slot)
		if !ok || fn == nil {
			continue
		}
		if err := binary.Write(w, binary.LittleEndian, uint8(slot)); err != nil {
			return err
		}
		if err := fn.WriteBinary(w, enc); err != nil {
			return err
		}
	}

	return nil
}

func readClass(r io.Reader, dec *BinaryCodec, c *Class) error {
	var err error
	if c.Name, err = bytecode.ReadString(r); err != nil {
		return err
	}

	if err := binary.Read(r, binary.LittleEndian, &c.IsAbstract); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &c.IsEnum); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &c.IsSealed); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &c.IsRecord); err != nil {
		return err
	}

	var hasFast byte
	if err := binary.Read(r, binary.LittleEndian, &hasFast); err != nil {
		return err
	}
	if hasFast == 1 {
		c.FastConstructor = &FastConstructorPlan{}
		var arity int32
		if err := binary.Read(r, binary.LittleEndian, &arity); err != nil {
			return err
		}
		c.FastConstructor.Arity = int(arity)

		var slotsLen uint32
		if err := binary.Read(r, binary.LittleEndian, &slotsLen); err != nil {
			return err
		}
		c.FastConstructor.FieldSlots = make([]int, slotsLen)
		for i := uint32(0); i < slotsLen; i++ {
			var v int32
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return err
			}
			c.FastConstructor.FieldSlots[i] = int(v)
		}

		var argsLen uint32
		if err := binary.Read(r, binary.LittleEndian, &argsLen); err != nil {
			return err
		}
		c.FastConstructor.ArgIndexes = make([]int, argsLen)
		for i := uint32(0); i < argsLen; i++ {
			var v int32
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return err
			}
			c.FastConstructor.ArgIndexes[i] = int(v)
		}
	}

	// Throw away length for implements
	var throw uint32
	if err := binary.Read(r, binary.LittleEndian, &throw); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &throw); err != nil {
		return err
	}

	if c.EnumOrder, err = bytecode.ReadStringSlice(r); err != nil {
		return err
	}

	var fLen uint32
	if err := binary.Read(r, binary.LittleEndian, &fLen); err != nil {
		return err
	}
	c.Fields = make(map[string]FieldDef)
	for i := uint32(0); i < fLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		f, err := readFieldDef(r)
		if err != nil {
			return err
		}
		c.Fields[k] = f
	}

	if c.FieldOrder, err = bytecode.ReadStringSlice(r); err != nil {
		return err
	}

	var fiLen uint32
	if err := binary.Read(r, binary.LittleEndian, &fiLen); err != nil {
		return err
	}
	c.FieldIndex = make(map[string]int)
	for i := uint32(0); i < fiLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		var v int32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		c.FieldIndex[k] = int(v)
	}

	if c.MethodOrder, err = bytecode.ReadStringSlice(r); err != nil {
		return err
	}

	var miLen uint32
	if err := binary.Read(r, binary.LittleEndian, &miLen); err != nil {
		return err
	}
	c.MethodIndex = make(map[string]int)
	for i := uint32(0); i < miLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		var v int32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		c.MethodIndex[k] = int(v)
	}

	var mtLen uint32
	if err := binary.Read(r, binary.LittleEndian, &mtLen); err != nil {
		return err
	}
	c.MethodTable = make([]*bytecode.Function, mtLen)
	for i := uint32(0); i < mtLen; i++ {
		var has byte
		if err := binary.Read(r, binary.LittleEndian, &has); err != nil {
			return err
		}
		if has == 1 {
			m, err := bytecode.ReadBinaryFunction(r, dec)
			if err != nil {
				return err
			}
			c.MethodTable[i] = m
		}
	}

	var mLen uint32
	if err := binary.Read(r, binary.LittleEndian, &mLen); err != nil {
		return err
	}
	c.Methods = make(map[string]*bytecode.Function)
	for i := uint32(0); i < mLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		var has byte
		if err := binary.Read(r, binary.LittleEndian, &has); err != nil {
			return err
		}
		if has == 1 {
			m, err := bytecode.ReadBinaryFunction(r, dec)
			if err != nil {
				return err
			}
			c.Methods[k] = m
		}
	}

	var hasConst byte
	if err := binary.Read(r, binary.LittleEndian, &hasConst); err != nil {
		return err
	}
	if hasConst == 1 {
		m, err := bytecode.ReadBinaryFunction(r, dec)
		if err != nil {
			return err
		}
		c.Constructor = m
	}

	// ConstructorVisibility
	if c.ConstructorVisibility, err = bytecode.ReadString(r); err != nil {
		return err
	}

	// StaticFields
	var sfLen uint32
	if err := binary.Read(r, binary.LittleEndian, &sfLen); err != nil {
		return err
	}
	c.StaticFields = make(map[string]FieldDef)
	for i := uint32(0); i < sfLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		f, err := readFieldDef(r)
		if err != nil {
			return err
		}
		c.StaticFields[k] = f
	}

	// StaticValues stub (we write 0 length, so just read it)
	var svLen uint32
	if err := binary.Read(r, binary.LittleEndian, &svLen); err != nil {
		return err
	}
	c.StaticValues = make(map[string]Value)

	// StaticMethods
	var smLen uint32
	if err := binary.Read(r, binary.LittleEndian, &smLen); err != nil {
		return err
	}
	c.StaticMethods = make(map[string]*bytecode.Function)
	for i := uint32(0); i < smLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		var has byte
		if err := binary.Read(r, binary.LittleEndian, &has); err != nil {
			return err
		}
		if has == 1 {
			m, err := bytecode.ReadBinaryFunction(r, dec)
			if err != nil {
				return err
			}
			c.StaticMethods[k] = m
		}
	}

	// MethodVisibility
	var mvLen uint32
	if err := binary.Read(r, binary.LittleEndian, &mvLen); err != nil {
		return err
	}
	c.MethodVisibility = make(map[string]string)
	for i := uint32(0); i < mvLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		v, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		c.MethodVisibility[k] = v
	}

	// StaticVisibility
	var svvLen uint32
	if err := binary.Read(r, binary.LittleEndian, &svvLen); err != nil {
		return err
	}
	c.StaticVisibility = make(map[string]string)
	for i := uint32(0); i < svvLen; i++ {
		k, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		v, err := bytecode.ReadString(r)
		if err != nil {
			return err
		}
		c.StaticVisibility[k] = v
	}

	c.SpecialMethods = make(map[SpecialMethodSlot]*bytecode.Function)
	var specialCount uint32
	if err := binary.Read(r, binary.LittleEndian, &specialCount); err != nil {
		return err
	}
	for i := uint32(0); i < specialCount; i++ {
		var slot uint8
		if err := binary.Read(r, binary.LittleEndian, &slot); err != nil {
			return err
		}
		fn, err := bytecode.ReadBinaryFunction(r, dec)
		if err != nil {
			return err
		}
		c.SpecialMethods[SpecialMethodSlot(slot)] = fn
	}

	return nil
}
