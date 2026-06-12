package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type fileHandle struct {
	file   *os.File
	reader *bufio.Reader
}

type bufferHandle struct {
	data []byte
	pos  int
}

func BuildIoModule() *RuntimeModule {
	builder := NewModuleBuilder("Io")

	builder.AddTypedFunction("read_file", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		data, err := os.ReadFile(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(data)), nil
	})

	builder.AddTypedFunction("write_file", []string{TypeString, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		if err := ensureParentDir(args[0].Str); err != nil {
			return value.NilValue(), err
		}
		if err := os.WriteFile(args[0].Str, []byte(args[1].Str), 0o644); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("append_file", []string{TypeString, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		if err := ensureParentDir(args[0].Str); err != nil {
			return value.NilValue(), err
		}
		file, err := os.OpenFile(args[0].Str, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return value.NilValue(), err
		}
		defer file.Close()
		if _, err := file.WriteString(args[1].Str); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("exists", []string{TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		_, err := os.Stat(args[0].Str)
		if err == nil {
			return value.BoolValue(true), nil
		}
		if os.IsNotExist(err) {
			return value.BoolValue(false), nil
		}
		return value.NilValue(), err
	})

	builder.AddTypedFunction("delete_path", []string{TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		if err := os.Remove(args[0].Str); err != nil && !os.IsNotExist(err) {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("mkdir", []string{TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		if err := os.MkdirAll(args[0].Str, 0o755); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("read_dir", []string{TypeString}, TypeArray, false, func(args []value.Value) (value.Value, error) {
		entries, err := os.ReadDir(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		items := make([]value.Value, len(names))
		for i, name := range names {
			items[i] = value.StringValue(name)
		}
		return value.ObjectValue(value.NewArray(items)), nil
	})

	builder.AddTypedFunction("is_dir", []string{TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		info, err := os.Stat(args[0].Str)
		if err != nil {
			if os.IsNotExist(err) {
				return value.BoolValue(false), nil
			}
			return value.NilValue(), err
		}
		return value.BoolValue(info.IsDir()), nil
	})

	builder.AddTypedFunction("is_file", []string{TypeString}, TypeBool, false, func(args []value.Value) (value.Value, error) {
		info, err := os.Stat(args[0].Str)
		if err != nil {
			if os.IsNotExist(err) {
				return value.BoolValue(false), nil
			}
			return value.NilValue(), err
		}
		return value.BoolValue(!info.IsDir()), nil
	})

	builder.AddTypedFunction("file_size", []string{TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		info, err := os.Stat(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.IntValue(info.Size()), nil
	})

	builder.AddTypedFunction("file_info", []string{TypeString}, TypeMap, false, func(args []value.Value) (value.Value, error) {
		info, err := os.Stat(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		entries := map[string]value.Value{
			"name":    value.StringValue(info.Name()),
			"size":    value.IntValue(info.Size()),
			"isDir":   value.BoolValue(info.IsDir()),
			"modTime": value.StringValue(info.ModTime().UTC().Format(time.RFC3339)),
		}
		return value.ObjectValue(&value.Map{Entries: entries}), nil
	})

	builder.AddTypedFunction("open_file", []string{TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := openFileHandle(args[0].Str, "r")
		if err != nil {
			return value.NilValue(), err
		}
		return value.ObjectValue(handle), nil
	})

	builder.AddTypedFunction("open_file_mode", []string{TypeString, TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := openFileHandle(args[0].Str, args[1].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.ObjectValue(handle), nil
	})

	builder.AddTypedFunction("file_read", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		handle, err := asFileHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		data, err := io.ReadAll(handle.reader)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(data)), nil
	})

	builder.AddTypedFunction("file_read_size", []string{TypeAny, TypeInt}, TypeString, false, func(args []value.Value) (value.Value, error) {
		handle, err := asFileHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		size := int(args[1].Num)
		if size <= 0 {
			return value.StringValue(""), nil
		}
		buf := make([]byte, size)
		n, err := handle.reader.Read(buf)
		if err != nil && err != io.EOF {
			return value.NilValue(), err
		}
		return value.StringValue(string(buf[:n])), nil
	})

	builder.AddTypedFunction("file_read_line", []string{TypeAny}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		handle, err := asFileHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		line, err := handle.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if line == "" {
					return value.NilValue(), nil
				}
			} else {
				return value.NilValue(), err
			}
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		return value.StringValue(line), nil
	})

	builder.AddTypedFunction("file_write", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asFileHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		if _, err := handle.file.WriteString(args[1].Str); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("file_close", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		handle, err := asFileHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		if err := handle.file.Close(); err != nil {
			return value.NilValue(), err
		}
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("buffer_new", []string{}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(&bufferHandle{data: []byte{}}), nil
	})

	builder.AddTypedFunction("buffer_new_with_string", []string{TypeString}, TypeAny, false, func(args []value.Value) (value.Value, error) {
		return value.ObjectValue(&bufferHandle{data: []byte(args[0].Str)}), nil
	})

	builder.AddTypedFunction("buffer_write", []string{TypeAny, TypeString}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		buffer, err := asBufferHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		buffer.data = append(buffer.data, []byte(args[1].Str)...)
		return value.NilValue(), nil
	})

	builder.AddTypedFunction("buffer_read", []string{TypeAny, TypeInt}, TypeString, false, func(args []value.Value) (value.Value, error) {
		buffer, err := asBufferHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		size := int(args[1].Num)
		if size <= 0 || buffer.pos >= len(buffer.data) {
			return value.StringValue(""), nil
		}
		end := buffer.pos + size
		if end > len(buffer.data) {
			end = len(buffer.data)
		}
		chunk := string(buffer.data[buffer.pos:end])
		buffer.pos = end
		return value.StringValue(chunk), nil
	})

	builder.AddTypedFunction("buffer_string", []string{TypeAny}, TypeString, false, func(args []value.Value) (value.Value, error) {
		buffer, err := asBufferHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(buffer.data)), nil
	})

	builder.AddTypedFunction("buffer_clear", []string{TypeAny}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		buffer, err := asBufferHandle(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		buffer.data = []byte{}
		buffer.pos = 0
		return value.NilValue(), nil
	})

	return builder.Build()
}

func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}

func openFileHandle(path string, mode string) (*fileHandle, error) {
	cleanMode := strings.TrimSpace(mode)
	if cleanMode == "" {
		cleanMode = "r"
	}
	var (
		file *os.File
		err  error
	)
	switch cleanMode {
	case "r":
		file, err = os.Open(path)
	case "w":
		if err := ensureParentDir(path); err != nil {
			return nil, err
		}
		file, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	case "a":
		if err := ensureParentDir(path); err != nil {
			return nil, err
		}
		file, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	case "rw", "r+", "w+":
		if err := ensureParentDir(path); err != nil {
			return nil, err
		}
		file, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	default:
		return nil, fmt.Errorf("unsupported file mode %q", cleanMode)
	}
	if err != nil {
		return nil, err
	}
	return &fileHandle{file: file, reader: bufio.NewReader(file)}, nil
}

func asFileHandle(v value.Value) (*fileHandle, error) {
	if v.Kind != value.Object {
		return nil, fmt.Errorf("expected file handle")
	}
	handle, ok := v.Object.(*fileHandle)
	if !ok {
		return nil, fmt.Errorf("expected file handle")
	}
	return handle, nil
}

func asBufferHandle(v value.Value) (*bufferHandle, error) {
	if v.Kind != value.Object {
		return nil, fmt.Errorf("expected buffer handle")
	}
	handle, ok := v.Object.(*bufferHandle)
	if !ok {
		return nil, fmt.Errorf("expected buffer handle")
	}
	return handle, nil
}
