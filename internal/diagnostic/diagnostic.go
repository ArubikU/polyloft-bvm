package diagnostic

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type Kind string

const (
	KindParse   Kind = "ParseError"
	KindCheck   Kind = "TypeError"
	KindCompile Kind = "CompileError"
	KindRuntime Kind = "RuntimeError"
)

type StackFrame struct {
	Function string
	Line     int
}

type Error struct {
	Kind      Kind
	TypeName  string
	Message   string
	File      string
	Line      int
	Column    int
	Hint      string
	Source    string
	Stack     []StackFrame
	HasCatch  bool
	Catch     value.Value
	Cause     error
	Printable string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Printable != "" {
		return e.Printable
	}
	if e.TypeName != "" {
		return fmt.Sprintf("%s: %s", e.TypeName, e.Message)
	}
	if e.Kind != "" {
		return fmt.Sprintf("%s: %s", e.Kind, e.Message)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "unknown error"
}

func (e *Error) CatchValue() value.Value {
	if e == nil {
		return value.NilValue()
	}
	if e.HasCatch {
		return e.Catch
	}
	return value.StringValue(e.Message)
}

func Wrap(err error, kind Kind, file string, source string) error {
	if err == nil {
		return nil
	}
	if diag, ok := err.(*Error); ok {
		if diag.Kind == "" {
			diag.Kind = kind
		}
		if diag.File == "" {
			diag.File = file
		}
		if diag.Source == "" {
			diag.Source = source
		}
		if diag.TypeName == "" && diag.Kind != "" {
			diag.TypeName = string(diag.Kind)
		}
		return diag
	}
	line, col, message := extractLocation(err.Error())
	return &Error{
		Kind:     kind,
		TypeName: string(kind),
		Message:  message,
		File:     file,
		Line:     line,
		Column:   col,
		Source:   source,
		Cause:    err,
	}
}

func Runtime(typeName string, message string, catch value.Value) *Error {
	if typeName == "" {
		typeName = string(KindRuntime)
	}
	return &Error{Kind: KindRuntime, TypeName: typeName, Message: message, Catch: catch, HasCatch: true}
}

func Format(err error) string {
	if err == nil {
		return ""
	}
	diag, ok := err.(*Error)
	if !ok {
		return err.Error()
	}
	parts := make([]string, 0, 6)
	header := diag.TypeName
	if header == "" {
		header = string(diag.Kind)
	}
	if header == "" {
		header = "Error"
	}
	parts = append(parts, fmt.Sprintf("%s: %s", header, diag.Message))
	if diag.File != "" || diag.Line > 0 {
		location := diag.File
		if location != "" {
			location = filepath.ToSlash(location)
		}
		if diag.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, diag.Line)
			if diag.Column > 0 {
				location = fmt.Sprintf("%s:%d", location, diag.Column)
			}
		}
		parts = append(parts, "at "+strings.TrimPrefix(location, ":"))
	}
	if context := formatContext(diag.Source, diag.Line, diag.Column); context != "" {
		parts = append(parts, context)
	}
	if diag.Hint != "" {
		parts = append(parts, "hint: "+diag.Hint)
	}
	if len(diag.Stack) > 0 {
		stack := make([]string, 0, len(diag.Stack)+1)
		stack = append(stack, "stack trace:")
		for _, frame := range diag.Stack {
			if frame.Line > 0 {
				stack = append(stack, fmt.Sprintf("  %s:%d", frame.Function, frame.Line))
				continue
			}
			stack = append(stack, "  "+frame.Function)
		}
		parts = append(parts, strings.Join(stack, "\n"))
	}
	return strings.Join(parts, "\n")
}

var linePattern = regexp.MustCompile(`^line\s+(\d+):(\d+):\s*(.*)$`)

func extractLocation(message string) (int, int, string) {
	match := linePattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 4 {
		return 0, 0, strings.TrimSpace(message)
	}
	var line int
	var col int
	fmt.Sscanf(match[1], "%d", &line)
	fmt.Sscanf(match[2], "%d", &col)
	return line, col, strings.TrimSpace(match[3])
}

func formatContext(source string, line int, column int) string {
	if source == "" || line <= 0 {
		return ""
	}
	lines := strings.Split(source, "\n")
	if line > len(lines) {
		return ""
	}
	text := lines[line-1]
	pointer := ""
	if column > 0 {
		pointer = strings.Repeat(" ", max(column-1, 0)) + "^"
	}
	if pointer == "" {
		return fmt.Sprintf("%4d | %s", line, text)
	}
	return fmt.Sprintf("%4d | %s\n     | %s", line, text, pointer)
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
