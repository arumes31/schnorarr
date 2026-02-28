package main

import (
	"fmt"
	"path/filepath"
)

func main() {
	curr := "C:\\data\\doku"
	curr = filepath.ToSlash(curr)
	for i := 0; i < 5; i++ {
		curr = filepath.Dir(curr)
		fmt.Printf("curr: %s\n", curr)
		if curr == "." || curr == "/" || curr == "" {
			fmt.Println("Break condition met")
			break
		}
	}
}
