package view

import "testing"

func TestUnits(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	for _, c := range []struct{ got, want string }{
		{CPU(i(250)), "250m"}, {CPU(i(9999)), "9999m"}, {CPU(i(16000)), "16.0"}, {CPU(nil), "?"}, {CPU(i(-500)), "-500m"},
		{Bytes(i(512)), "512B"}, {Bytes(i(1536)), "1.5Ki"}, {Bytes(i(34359738368)), "32.0Gi"}, {Bytes(nil), "?"},
		{Days(ptr(0)), "0d"}, {Days(ptr(-3)), "3d ago"}, {Money(nil), "unpriced"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
