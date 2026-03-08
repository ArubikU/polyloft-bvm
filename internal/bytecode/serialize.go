package bytecode

import (
	"encoding/binary"
	"io"
)

type ConstantEncoder interface {
	EncodeConstant(w io.Writer, c any) error
}

type ConstantDecoder interface {
	DecodeConstant(r io.Reader) (any, error)
}

func WriteString(w io.Writer, s string) error {
	b := []byte(s)
	if err := binary.Write(w, binary.LittleEndian, uint32(len(b))); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func ReadString(r io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func WriteStringSlice(w io.Writer, s []string) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(s))); err != nil {
		return err
	}
	for _, str := range s {
		if err := WriteString(w, str); err != nil {
			return err
		}
	}
	return nil
}

func ReadStringSlice(r io.Reader) ([]string, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	s := make([]string, length)
	for i := uint32(0); i < length; i++ {
		str, err := ReadString(r)
		if err != nil {
			return nil, err
		}
		s[i] = str
	}
	return s, nil
}

func (f *Function) WriteBinary(w io.Writer, enc ConstantEncoder) error {
	if err := WriteString(w, f.Name); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, int32(f.Arity)); err != nil {
		return err
	}
	if err := WriteStringSlice(w, f.ParamTypes); err != nil {
		return err
	}
	if err := WriteString(w, f.OwnerClassName); err != nil {
		return err
	}

	// Upvalues
	if err := binary.Write(w, binary.LittleEndian, uint32(len(f.Upvalues))); err != nil {
		return err
	}
	for _, uv := range f.Upvalues {
		if err := binary.Write(w, binary.LittleEndian, uv.Index); err != nil {
			return err
		}
		if uv.IsLocal {
			if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
				return err
			}
		} else {
			if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
				return err
			}
		}
	}

	if err := WriteString(w, f.ReturnType); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, int32(f.MaxLocals)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, int32(f.GlobalSlotCount)); err != nil {
		return err
	}
	if err := WriteStringSlice(w, f.GlobalSlotNames); err != nil {
		return err
	}

	// Interfaces map
	if err := binary.Write(w, binary.LittleEndian, uint32(len(f.Interfaces))); err != nil {
		return err
	}
	for k, v := range f.Interfaces {
		if err := WriteString(w, k); err != nil {
			return err
		}
		if err := WriteString(w, v.Name); err != nil {
			return err
		}
		if err := WriteString(w, v.FunctionalMethod); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(v.Methods))); err != nil {
			return err
		}
		for mk, mv := range v.Methods {
			if err := WriteString(w, mk); err != nil {
				return err
			}
			if err := WriteString(w, mv.Name); err != nil {
				return err
			}
			if err := WriteStringSlice(w, mv.Params); err != nil {
				return err
			}
			if err := WriteString(w, mv.Return); err != nil {
				return err
			}
		}
	}

	// Chunk
	if f.Chunk == nil {
		if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
			return err
		}
	} else {
		if err := binary.Write(w, binary.LittleEndian, byte(1)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(f.Chunk.Code))); err != nil {
			return err
		}
		if _, err := w.Write(f.Chunk.Code); err != nil {
			return err
		}

		if err := binary.Write(w, binary.LittleEndian, uint32(len(f.Chunk.Lines))); err != nil {
			return err
		}
		for _, l := range f.Chunk.Lines {
			if err := binary.Write(w, binary.LittleEndian, int32(l)); err != nil {
				return err
			}
		}

		if err := binary.Write(w, binary.LittleEndian, uint32(len(f.Chunk.Constants))); err != nil {
			return err
		}
		for _, c := range f.Chunk.Constants {
			if err := enc.EncodeConstant(w, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func ReadBinaryFunction(r io.Reader, dec ConstantDecoder) (*Function, error) {
	f := &Function{}
	var err error

	if f.Name, err = ReadString(r); err != nil {
		return nil, err
	}

	var arity int32
	if err := binary.Read(r, binary.LittleEndian, &arity); err != nil {
		return nil, err
	}
	f.Arity = int(arity)

	if f.ParamTypes, err = ReadStringSlice(r); err != nil {
		return nil, err
	}
	if f.OwnerClassName, err = ReadString(r); err != nil {
		return nil, err
	}

	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	f.Upvalues = make([]UpvalueSpec, length)
	for i := uint32(0); i < length; i++ {
		var index byte
		if err := binary.Read(r, binary.LittleEndian, &index); err != nil {
			return nil, err
		}
		var isLocalByte byte
		if err := binary.Read(r, binary.LittleEndian, &isLocalByte); err != nil {
			return nil, err
		}
		f.Upvalues[i] = UpvalueSpec{Index: index, IsLocal: isLocalByte == 1}
	}

	if f.ReturnType, err = ReadString(r); err != nil {
		return nil, err
	}

	var maxLocals int32
	if err := binary.Read(r, binary.LittleEndian, &maxLocals); err != nil {
		return nil, err
	}
	f.MaxLocals = int(maxLocals)

	var gCount int32
	if err := binary.Read(r, binary.LittleEndian, &gCount); err != nil {
		return nil, err
	}
	f.GlobalSlotCount = int(gCount)

	if f.GlobalSlotNames, err = ReadStringSlice(r); err != nil {
		return nil, err
	}

	var ifaceLen uint32
	if err := binary.Read(r, binary.LittleEndian, &ifaceLen); err != nil {
		return nil, err
	}
	f.Interfaces = make(map[string]InterfaceSpec)
	for i := uint32(0); i < ifaceLen; i++ {
		k, err := ReadString(r)
		if err != nil {
			return nil, err
		}

		var spec InterfaceSpec
		if spec.Name, err = ReadString(r); err != nil {
			return nil, err
		}
		if spec.FunctionalMethod, err = ReadString(r); err != nil {
			return nil, err
		}

		var mLen uint32
		if err := binary.Read(r, binary.LittleEndian, &mLen); err != nil {
			return nil, err
		}
		spec.Methods = make(map[string]InterfaceMethodSpec)
		for j := uint32(0); j < mLen; j++ {
			mk, err := ReadString(r)
			if err != nil {
				return nil, err
			}
			var mv InterfaceMethodSpec
			if mv.Name, err = ReadString(r); err != nil {
				return nil, err
			}
			if mv.Params, err = ReadStringSlice(r); err != nil {
				return nil, err
			}
			if mv.Return, err = ReadString(r); err != nil {
				return nil, err
			}
			spec.Methods[mk] = mv
		}
		f.Interfaces[k] = spec
	}

	var hasChunk byte
	if err := binary.Read(r, binary.LittleEndian, &hasChunk); err != nil {
		return nil, err
	}
	if hasChunk == 1 {
		f.Chunk = &Chunk{}
		var codeLen uint32
		if err := binary.Read(r, binary.LittleEndian, &codeLen); err != nil {
			return nil, err
		}
		f.Chunk.Code = make([]byte, codeLen)
		if _, err := io.ReadFull(r, f.Chunk.Code); err != nil {
			return nil, err
		}

		var linesLen uint32
		if err := binary.Read(r, binary.LittleEndian, &linesLen); err != nil {
			return nil, err
		}
		f.Chunk.Lines = make([]int, linesLen)
		for i := uint32(0); i < linesLen; i++ {
			var ln int32
			if err := binary.Read(r, binary.LittleEndian, &ln); err != nil {
				return nil, err
			}
			f.Chunk.Lines[i] = int(ln)
		}

		var consLen uint32
		if err := binary.Read(r, binary.LittleEndian, &consLen); err != nil {
			return nil, err
		}
		f.Chunk.Constants = make([]any, consLen)
		for i := uint32(0); i < consLen; i++ {
			c, err := dec.DecodeConstant(r)
			if err != nil {
				return nil, err
			}
			f.Chunk.Constants[i] = c
		}
	}

	return f, nil
}
