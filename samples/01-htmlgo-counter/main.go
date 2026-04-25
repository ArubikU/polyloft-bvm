package main

import (
	"fmt"
	"path/filepath"

	"github.com/ArubikU/giocss"
)

type AppState struct {
	Count int
}

func dispatch(state *AppState, event string) {
	switch event {
	case "inc":
		state.Count++
	case "dec":
		state.Count--
	}
}

func main() {
	ss := giocss.NewStyleSheet()
	cssPath := filepath.Join("samples", "01-htmlgo-counter", "style.css")
	if _, err := ss.LoadFile(cssPath); err != nil {
		panic(err)
	}

	state := AppState{Count: 0}

	fmt.Println("== Initial render ==")
	renderTree(buildUI(state), ss, 0)

	dispatch(&state, "inc")
	fmt.Println("\n== After click: inc ==")
	renderTree(buildUI(state), ss, 0)

	dispatch(&state, "inc")
	dispatch(&state, "dec")
	fmt.Println("\n== After click: inc, dec ==")
	renderTree(buildUI(state), ss, 0)
}
