//go:build ignore

package main

import (
	"archive/zip"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: listzip <file>")
		os.Exit(1)
	}
	r, err := zip.OpenReader(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer r.Close()
	for _, f := range r.File {
		fmt.Println(f.Name)
	}
}
