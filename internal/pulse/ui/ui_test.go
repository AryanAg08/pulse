package ui

import "testing"

func TestWidthIgnoresEscapeCodes(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	if got := Width(Red("abc")); got != 3 {
		t.Fatalf("styled text must measure by printable width, got %d", got)
	}
}

func TestPadAlignsStyledText(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	if got := Width(Pad(Green("ab"), 6)); got != 6 {
		t.Fatalf("want printable width 6, got %d", got)
	}
}

func TestStylingOffEmitsNoEscapeCodes(t *testing.T) {
	SetEnabled(false)
	for name, got := range map[string]string{
		"Red":    Red("x"),
		"Bold":   Bold("x"),
		"Header": Header("x"),
		"Bar":    Bar(0.5, 4, Red),
		"Symbol": Symbol("ok"),
		"Rule":   Rule(4),
	} {
		for _, r := range got {
			if r == '\x1b' {
				t.Errorf("%s leaked an escape code into plain output: %q", name, got)
				break
			}
		}
	}
}

func TestBarClampsOutOfRange(t *testing.T) {
	SetEnabled(false)
	if got := Bar(-1, 4, Red); got != "[    ]" {
		t.Errorf("negative should clamp empty, got %q", got)
	}
	if got := Bar(2, 4, Red); got != "[====]" {
		t.Errorf("over 1 should clamp full, got %q", got)
	}
}

func TestSeverityRanksByPriority(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	// A red build and a dead PR must not look alike at a glance.
	if Severity(90)("x") == Severity(40)("x") {
		t.Fatal("high and low priority should render differently")
	}
}

func TestTruncateKeepsTableWidth(t *testing.T) {
	SetEnabled(false)
	if got := Truncate("abcdefgh", 5); len([]rune(got)) != 5 {
		t.Fatalf("want 5 runes, got %q", got)
	}
	if got := Truncate("abc", 5); got != "abc" {
		t.Fatalf("short strings pass through, got %q", got)
	}
}
