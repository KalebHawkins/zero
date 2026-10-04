package life

import "testing"

func TestWrap(t *testing.T) {
	for _, tc := range []struct{ i, n, want int }{{-1, 3, 2}, {3, 3, 0}, {1, 3, 1}} {
		if got := Wrap(tc.i, tc.n); got != tc.want {
			t.Errorf("Wrap(%d, %d) = %d, want %d", tc.i, tc.n, got, tc.want)
		}
	}
}
