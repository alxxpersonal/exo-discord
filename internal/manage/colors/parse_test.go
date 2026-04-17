package colors

import "testing"

func TestParseOptionalHexColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    *int
		wantErr string
	}{
		{name: "blank", value: "", want: nil},
		{name: "hash-prefix", value: "#123456", want: intPointer(0x123456)},
		{name: "no-prefix", value: "abcdef", want: intPointer(0xabcdef)},
		{name: "invalid-length", value: "#12", wantErr: `invalid color "#12"`},
		{name: "invalid-rune", value: "#12gg56", wantErr: `invalid color "#12gg56"`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOptionalHexColor(test.value)
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("ParseOptionalHexColor() err = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOptionalHexColor() error = %v", err)
			}
			if !equalPointers(got, test.want) {
				t.Fatalf("ParseOptionalHexColor() = %v, want %v", pointerValue(got), pointerValue(test.want))
			}
		})
	}
}

func intPointer(value int) *int {
	return &value
}

func equalPointers(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func pointerValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
