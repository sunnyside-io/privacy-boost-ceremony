package app

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReceiptPreferencePreservesExplicitChoice(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"y\n", true}, {" YES \n", true}, {"n\n", false}, {"No\n", false},
		{"\ninvalid\nyes\n", true}, {"wrong\nno\n", false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := readReceiptPreference(strings.NewReader(tc.input))
			if err != nil || got != tc.want {
				t.Fatalf("choice = %t, error = %v, want %t", got, err, tc.want)
			}
		})
	}
}

func TestReceiptPreferenceRejectsUnavailableInput(t *testing.T) {
	for _, text := range []string{"", "invalid\n"} {
		if _, err := readReceiptPreference(strings.NewReader(text)); err == nil {
			t.Fatal("EOF without a choice must not select artifact cleanup")
		}
	}
	sentinel := errors.New("input unavailable")
	if _, err := readReceiptPreference(iotest.ErrReader(sentinel)); !errors.Is(err, sentinel) {
		t.Fatalf("reader error = %v, want original error", err)
	}
}
