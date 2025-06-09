package parser

import (
	"testing"
)

func TestPlural2Singular(t *testing.T) {
	tests := []struct {
		in       string
		want     string
		wantBool bool
		desc     string
	}{
		// Irregulars
		{"men", "man", true, "irregular plural men"},
		{"women", "woman", true, "irregular plural women"},
		{"children", "child", true, "irregular plural children"},
		{"feet", "foot", true, "irregular plural feet"},
		{"teeth", "tooth", true, "irregular plural teeth"},
		{"geese", "goose", true, "irregular plural geese"},
		{"mice", "mouse", true, "irregular plural mice"},
		{"people", "person", true, "irregular plural people"},

		// -ies -> y
		{"parties", "party", true, "-ies to y"},
		{"stories", "story", true, "-ies to y"},
		{"flies", "fly", true, "-ies to y"},

		// -es endings
		{"boxes", "box", true, "-xes to x"},
		{"wishes", "wish", true, "-shes to sh"},
		{"buses", "bus", true, "-ses to s"},
		{"benches", "bench", true, "-ches to ch"},

		// -s endings
		{"cats", "cat", true, "-s to singular"},
		{"dogs", "dog", true, "-s to singular"},
		{"cars", "car", true, "-s to singular"},

		// Already singular
		{"dog", "dog", false, "already singular"},
		{"bus", "bus", false, "already singular"},
		{"quiz", "quiz", false, "already singular"},

		// Edge cases
		{"s", "s", false, "single letter s"},
		{"", "", false, "empty string"},
		{"ies", "ies", false, "just 'ies'"},
	}

	for _, tc := range tests {
		got, ok := Plural2Singular(tc.in)
		if got != tc.want || ok != tc.wantBool {
			t.Errorf("%s: Plural2Singular(%q) = (%q, %v), want (%q, %v)",
				tc.desc, tc.in, got, ok, tc.want, tc.wantBool)
		}
	}
}
