package life

import "testing"

func TestNext(t *testing.T) {
	for _, tc := range []struct {
		alive     bool
		neighbors int
		want      bool
	}{
		{false, 3, true}, {true, 2, true}, {true, 3, true},
		{false, 2, false}, {true, 1, false}, {true, 4, false},
	} {
		if got := Next(tc.alive, tc.neighbors); got != tc.want {
			t.Errorf("Next(%v, %d) = %v, want %v", tc.alive, tc.neighbors, got, tc.want)
		}
	}
}
