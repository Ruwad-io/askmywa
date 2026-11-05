package textfold

import "testing"

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Élodie":            "elodie",
		"  ÇA VA  ":         "ca va",
		"Comptable Docteur": "comptable docteur",
		"naïve café":        "naive cafe",
		"Stephanie♥️":       "stephanie♥", // the variation selector is a nonspacing mark
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDigits(t *testing.T) {
	if got := Digits("+33 6 12-34.56"); got != "336123456" {
		t.Errorf("Digits = %q", got)
	}
	if !IsDigits("33612345678") || IsDigits("") || IsDigits("33a") {
		t.Error("IsDigits mismatch")
	}
}
