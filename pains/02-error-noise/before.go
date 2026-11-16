// Go makes errors visible, but every call costs three lines of plumbing,
// so people reach for the blank identifier and the error disappears.
package main

import (
	"fmt"
	"strconv"
)

func total(items []string) (int, error) {
	sum := 0
	for _, s := range items {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		sum += n
	}
	return sum, nil
}

func main() {
	t, err := total([]string{"1", "2", "3"})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("total:", t)

	n, _ := strconv.Atoi("seven") // the error is thrown away
	fmt.Println("parsed:", n)     // prints 0 as if nothing happened
}
