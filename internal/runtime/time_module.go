package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func resolveTimeLocation(zone string) (*time.Location, error) {
	trimmed := strings.TrimSpace(zone)
	if trimmed == "" || strings.EqualFold(trimmed, "local") {
		return time.Local, nil
	}
	if strings.EqualFold(trimmed, "utc") || strings.EqualFold(trimmed, "z") {
		return time.UTC, nil
	}
	if strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") {
		offset, err := parseOffsetMinutes(trimmed)
		if err != nil {
			return nil, err
		}
		name := "UTC" + trimmed
		return time.FixedZone(name, offset*60), nil
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q", trimmed)
	}
	return loc, nil
}

func parseOffsetMinutes(raw string) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty offset")
	}
	sign := 1
	if raw[0] == '-' {
		sign = -1
	} else if raw[0] != '+' {
		return 0, fmt.Errorf("offset must start with + or -")
	}
	body := raw[1:]
	var hours int
	var minutes int
	if strings.Contains(body, ":") {
		parts := strings.Split(body, ":")
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid offset format")
		}
		h, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid offset hour")
		}
		m, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("invalid offset minutes")
		}
		hours = h
		minutes = m
	} else {
		if len(body) != 2 && len(body) != 4 {
			return 0, fmt.Errorf("invalid offset format")
		}
		h, err := strconv.Atoi(body[0:2])
		if err != nil {
			return 0, fmt.Errorf("invalid offset hour")
		}
		hours = h
		if len(body) == 4 {
			m, err := strconv.Atoi(body[2:4])
			if err != nil {
				return 0, fmt.Errorf("invalid offset minutes")
			}
			minutes = m
		}
	}
	if hours < 0 || hours > 23 || minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("offset out of range")
	}
	return sign * (hours*60 + minutes), nil
}

func timeArgsMillis(v value.Value) (int64, error) {
	if v.Kind != value.Number {
		return 0, fmt.Errorf("millis must be numeric")
	}
	if v.NumberKind == value.NumberInt {
		return v.Int, nil
	}
	return int64(v.Num), nil
}

func timePartsMap(t time.Time) value.Value {
	zoneName, zoneOffsetSec := t.Zone()
	entries := map[string]value.Value{
		"year":         value.IntValue(int64(t.Year())),
		"month":        value.IntValue(int64(t.Month())),
		"day":          value.IntValue(int64(t.Day())),
		"hour":         value.IntValue(int64(t.Hour())),
		"minute":       value.IntValue(int64(t.Minute())),
		"second":       value.IntValue(int64(t.Second())),
		"millisecond":  value.IntValue(int64(t.Nanosecond() / int(time.Millisecond))),
		"weekday":      value.IntValue(int64(t.Weekday())),
		"zone":         value.StringValue(zoneName),
		"offsetMinutes": value.IntValue(int64(zoneOffsetSec / 60)),
		"unixMillis":   value.IntValue(t.UnixMilli()),
		"isoDate":      value.StringValue(t.Format("2006-01-02")),
		"isoTime":      value.StringValue(t.Format("15:04:05")),
		"isoDateTime":  value.StringValue(t.Format(time.RFC3339Nano)),
	}
	return value.ObjectValue(&value.Map{Entries: entries})
}

func BuildTimeModule() *RuntimeModule {
	builder := NewModuleBuilder("Time")

	builder.AddTypedFunction("now_millis", []string{}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		return value.IntValue(time.Now().UnixMilli()), nil
	})

	builder.AddTypedFunction("now_iso", []string{}, TypeString, false, func(args []value.Value) (value.Value, error) {
		return value.StringValue(time.Now().Format(time.RFC3339Nano)), nil
	})

	builder.AddTypedFunction("zone_default", []string{}, TypeString, false, func(args []value.Value) (value.Value, error) {
		name, _ := time.Now().Zone()
		if strings.TrimSpace(name) == "" {
			name = "Local"
		}
		return value.StringValue(name), nil
	})

	builder.AddTypedFunction("zone_offset_minutes", []string{TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		loc, err := resolveTimeLocation(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		_, offset := time.Now().In(loc).Zone()
		return value.IntValue(int64(offset / 60)), nil
	})

	builder.AddTypedFunction("now_in_zone_iso", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		loc, err := resolveTimeLocation(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(time.Now().In(loc).Format(time.RFC3339Nano)), nil
	})

	builder.AddTypedFunction("date_today", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		loc, err := resolveTimeLocation(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(time.Now().In(loc).Format("2006-01-02")), nil
	})

	builder.AddTypedFunction("date_from_millis", []string{TypeInt, TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		ms, err := timeArgsMillis(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		loc, err := resolveTimeLocation(args[1].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(time.UnixMilli(ms).In(loc).Format("2006-01-02")), nil
	})

	builder.AddTypedFunction("time_from_millis", []string{TypeInt, TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		ms, err := timeArgsMillis(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		loc, err := resolveTimeLocation(args[1].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(time.UnixMilli(ms).In(loc).Format("15:04:05")), nil
	})

	builder.AddTypedFunction("datetime_from_millis", []string{TypeInt, TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		ms, err := timeArgsMillis(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		loc, err := resolveTimeLocation(args[1].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(time.UnixMilli(ms).In(loc).Format(time.RFC3339Nano)), nil
	})

	builder.AddTypedFunction("parts", []string{TypeInt, TypeString}, TypeMap, false, func(args []value.Value) (value.Value, error) {
		ms, err := timeArgsMillis(args[0])
		if err != nil {
			return value.NilValue(), err
		}
		loc, err := resolveTimeLocation(args[1].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return timePartsMap(time.UnixMilli(ms).In(loc)), nil
	})

	builder.AddTypedFunction("parse_iso", []string{TypeString}, TypeInt, false, func(args []value.Value) (value.Value, error) {
		text := strings.TrimSpace(args[0].Str)
		if text == "" {
			return value.NilValue(), fmt.Errorf("iso text cannot be empty")
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return value.IntValue(parsed.UnixMilli()), nil
			}
		}
		return value.NilValue(), fmt.Errorf("invalid iso datetime %q", text)
	})

	return builder.Build()
}
