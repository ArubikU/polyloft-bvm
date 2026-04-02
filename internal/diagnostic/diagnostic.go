package diagnostic

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Kind string

const (
	KindParse   Kind = "parse"
	KindCheck   Kind = "check"
	KindCompile Kind = "compile"
	KindRuntime Kind = "runtime"
)

type StackFrame struct {
	Function string
	Line     int
}

type Error struct {
	Kind     Kind
	TypeName string
	Message  string
	Path     string
	Source   string
	Line     int
	Column   int
	Hint     string
	Stack    []StackFrame

	Catch    value.Value
	HasCatch bool

	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Path != "" {
		if e.Line > 0 && e.Column > 0 {
			return fmt.Sprintf("%s:%d:%d: %s", e.Path, e.Line, e.Column, e.Message)
		}
		if e.Line > 0 {
			return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Message)
		}
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "diagnostic error"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) CatchValue() value.Value {
	if e != nil && e.HasCatch {
		return e.Catch
	}
	if e != nil {
		return value.StringValue(e.Message)
	}
	return value.NilValue()
}

func Runtime(typeName string, message string, catch value.Value) *Error {
	if strings.TrimSpace(typeName) == "" {
		typeName = "RuntimeError"
	}
	return &Error{
		Kind:     KindRuntime,
		TypeName: typeName,
		Message:  strings.TrimSpace(message),
		Catch:    catch,
		HasCatch: true,
	}
}

func Wrap(err error, kind Kind, path string, source string) error {
	if err == nil {
		return nil
	}
	if de, ok := err.(*Error); ok {
		cloned := *de
		if cloned.Kind == "" {
			cloned.Kind = kind
		}
		if cloned.TypeName == "" {
			cloned.TypeName = string(cloned.Kind)
		}
		if cloned.Path == "" {
			cloned.Path = path
		}
		if cloned.Source == "" {
			cloned.Source = source
		}
		if cloned.Line == 0 {
			line, col := extractLineColumn(cloned.Message)
			cloned.Line = line
			cloned.Column = col
		}
		if cloned.Message == "" && cloned.Cause != nil {
			cloned.Message = cloned.Cause.Error()
		}
		if cloned.Cause == nil {
			cloned.Cause = err
		}
		return &cloned
	}

	message := strings.TrimSpace(err.Error())
	line, col := extractLineColumn(message)
	return &Error{
		Kind:     kind,
		TypeName: string(kind),
		Message:  message,
		Path:     path,
		Source:   source,
		Line:     line,
		Column:   col,
		Cause:    err,
	}
}

func Format(err error) string {
	if err == nil {
		return ""
	}
	de, ok := err.(*Error)
	if !ok {
		return err.Error()
	}

	parts := make([]string, 0, 8)
	kind := strings.TrimSpace(string(de.Kind))
	if kind != "" {
		parts = append(parts, fmt.Sprintf("[%s] %s", strings.ToUpper(kind), de.Message))
	} else {
		parts = append(parts, de.Message)
	}

	if de.Path != "" {
		if de.Line > 0 && de.Column > 0 {
			parts = append(parts, fmt.Sprintf("at %s:%d:%d", de.Path, de.Line, de.Column))
		} else if de.Line > 0 {
			parts = append(parts, fmt.Sprintf("at %s:%d", de.Path, de.Line))
		} else {
			parts = append(parts, fmt.Sprintf("at %s", de.Path))
		}
	}

	if de.Hint != "" {
		parts = append(parts, "hint: "+de.Hint)
	}

	if len(de.Stack) > 0 {
		parts = append(parts, "stack:")
		for _, frame := range de.Stack {
			if frame.Line > 0 {
				parts = append(parts, fmt.Sprintf("  at %s:%d", frame.Function, frame.Line))
			} else {
				parts = append(parts, fmt.Sprintf("  at %s", frame.Function))
			}
		}
	}

	return strings.Join(parts, "\n")
}

var lineColRegex = regexp.MustCompile(`line\s+(\d+)(?::(\d+))?`)

func extractLineColumn(message string) (int, int) {
	m := lineColRegex.FindStringSubmatch(strings.ToLower(message))
	if len(m) == 0 {
		return 0, 0
	}
	line, _ := strconv.Atoi(m[1])
	col := 0
	if len(m) > 2 && m[2] != "" {
		col, _ = strconv.Atoi(m[2])
	}
	return line, col
}
