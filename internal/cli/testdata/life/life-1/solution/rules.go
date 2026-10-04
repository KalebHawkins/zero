package life

// Next says whether a cell is alive in the next generation.
func Next(alive bool, neighbors int) bool {
	return neighbors == 3 || (alive && neighbors == 2)
}
