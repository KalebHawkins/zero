package life

import "testing"

func TestCount(t *testing.T) {
	grid := [][]bool{
		{true, false, false},
		{false, true, false},
		{false, false, true},
	}
	for _, tc := range []struct{ r, c, want int }{
		{1, 1, 2}, {0, 0, 2}, {0, 1, 3}, {2, 0, 3},
	} {
		if got := Count(grid, tc.r, tc.c); got != tc.want {
			t.Errorf("Count(grid, %d, %d) = %d, want %d", tc.r, tc.c, got, tc.want)
		}
	}
}
