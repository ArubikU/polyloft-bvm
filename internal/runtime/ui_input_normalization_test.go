package runtime

import "testing"

func TestInputExternalValueSupportsNumericValue(t *testing.T) {
	props := map[string]any{"value": -1}
	got := inputExternalValue(props)
	if got != "-1" {
		t.Fatalf("inputExternalValue() = %q, want %q", got, "-1")
	}
}

func TestNormalizeNumberInputFinalizeClampsAndRespectsStep(t *testing.T) {
	props := map[string]any{"min": 0, "max": 10, "step": 0.5}
	got := normalizeNumberInput("10.7", props, true)
	if got != "10" {
		t.Fatalf("normalizeNumberInput() = %q, want %q", got, "10")
	}
}

func TestNormalizeNumberInputLiveAllowsPartial(t *testing.T) {
	got := normalizeNumberInput("-", map[string]any{}, false)
	if got != "-" {
		t.Fatalf("normalizeNumberInput(live) = %q, want %q", got, "-")
	}
}

func TestNormalizeDateInputFinalize(t *testing.T) {
	got := normalizeDateInput("2026/03/17", true)
	if got != "2026-03-17" {
		t.Fatalf("normalizeDateInput() = %q, want %q", got, "2026-03-17")
	}
}

func TestNormalizeDateInputRejectsInvalid(t *testing.T) {
	got := normalizeDateInput("2026-99-99", true)
	if got != "" {
		t.Fatalf("normalizeDateInput(invalid) = %q, want empty", got)
	}
}

func TestNormalizeTimeInputFinalizeWithSecondsStep(t *testing.T) {
	props := map[string]any{"step": 1}
	got := normalizeTimeInput("02:30", props, true)
	if got != "02:30:00" {
		t.Fatalf("normalizeTimeInput() = %q, want %q", got, "02:30:00")
	}
}

func TestNormalizeTimeInputRejectsInvalid(t *testing.T) {
	got := normalizeTimeInput("25:99", map[string]any{}, true)
	if got != "" {
		t.Fatalf("normalizeTimeInput(invalid) = %q, want empty", got)
	}
}
