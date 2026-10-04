package life

// Wrap folds i into the range 0 to n-1, so -1 becomes n-1 and n becomes 0.
func Wrap(i, n int) int {
	return ((i % n) + n) % n
}
