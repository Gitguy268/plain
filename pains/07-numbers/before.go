// Integer division drops the fraction without a word.
package main

import "fmt"

func main() {
	people, pizzas := 4, 7
	fmt.Println(pizzas / people) // 1, not 1.75
}
