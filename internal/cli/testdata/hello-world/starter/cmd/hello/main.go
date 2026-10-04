// Command hello prints the greeting. `zero run` runs it.
package main

import (
	"fmt"

	"hello"
)

func main() {
	fmt.Println(hello.Hello())
}
