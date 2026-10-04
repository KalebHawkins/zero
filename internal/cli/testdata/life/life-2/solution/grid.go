package life

// Count returns how many of the eight neighbors of row r, column c are
// alive. The edges of the grid wrap around.
func Count(grid [][]bool, r, c int) int {
	n := 0
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			if dr == 0 && dc == 0 {
				continue
			}
			row := grid[Wrap(r+dr, len(grid))]
			if row[Wrap(c+dc, len(row))] {
				n++
			}
		}
	}
	return n
}
