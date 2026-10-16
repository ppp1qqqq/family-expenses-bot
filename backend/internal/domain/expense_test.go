package domain

import "testing"

func TestParseAmount(t *testing.T) {
	ok := map[string]int64{
		"350":     35000,
		"1200.5":  120050,
		"99,90":   9990,
		" 0.01 ":  1,
		"1000000": 100000000,
	}
	for in, want := range ok {
		got, err := ParseAmount(in)
		if err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-5", "-0.5", "1.234", "abc", "1.", ".5", "1e3", "1.-5"} {
		if _, err := ParseAmount(in); err == nil {
			t.Errorf("ParseAmount(%q) expected error", in)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := map[int64]string{35000: "350", 35050: "350.50", 1: "0.01"}
	for in, want := range cases {
		if got := FormatAmount(in); got != want {
			t.Errorf("FormatAmount(%d) = %q; want %q", in, got, want)
		}
	}
}
