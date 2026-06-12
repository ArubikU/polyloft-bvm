package main

import (
	"fmt"
	"os"

	lua "github.com/yuin/gopher-lua"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: luabench <file.lua>")
		os.Exit(1)
	}
	L := lua.NewState()
	defer L.Close()
	if err := L.DoFile(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
