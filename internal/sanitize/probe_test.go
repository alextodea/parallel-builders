package sanitize

import "testing"

func TestJSONAndProseBoundary(t *testing.T) {
	redact := map[string]bool{
		`{"api_key": "aB3dEf9hJkLmNpQr"}`:   true,
		`{"password":"hunter2hunter2"}`:     true,
		"the password is stored hashed":     false,
		`"note": "reset the token counter"`: false, // "token" here is prose
	}
	for in, want := range redact {
		_, n := Redact(in)
		if (n > 0) != want {
			t.Errorf("Redact(%q): redacted=%v, want %v", in, n > 0, want)
		}
	}
}
