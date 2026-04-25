package main

import (
	"fmt"
	"strings"

	"github.com/ArubikU/giocss"
)

func Div(classes string, children ...*giocss.Node) *giocss.Node {
	n := giocss.NewNode("div")
	n.SetProp("class", classes)
	for _, child := range children {
		n.AddChild(child)
	}
	return n
}

func Text(tag, classes, value string) *giocss.Node {
	n := giocss.NewNode(tag)
	n.SetProp("class", classes)
	n.Text = value
	return n
}

func Button(classes, label, onClick string) *giocss.Node {
	n := giocss.NewNode("button")
	n.SetProp("class", classes)
	n.SetProp("onClick", onClick)
	n.Text = label
	return n
}

func buildUI(state AppState) *giocss.Node {
	return Div("app",
		Text("h1", "title", "Giocss HTML-like UI"),
		Text("p", "subtitle", "Go builder + CSS externo + estado interactivo"),
		Div("card",
			Text("p", "count", fmt.Sprintf("%d", state.Count)),
			Div("actions",
				Button("button", "Increment", "inc"),
				Button("button", "Decrement", "dec"),
			),
		),
	)
}

func renderTree(node *giocss.Node, ss *giocss.StyleSheet, depth int) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	props := map[string]any{}
	for k, v := range node.Props {
		props[k] = v
	}
	props["tag"] = node.Tag
	css := giocss.ResolveStyle(props, ss, 1280)
	fmt.Printf("%s<%s class=\"%v\"> text=%q css.color=%q css.background=%q\n",
		indent,
		node.Tag,
		node.GetProp("class"),
		node.Text,
		css["color"],
		css["background"],
	)
	for _, child := range node.Children {
		renderTree(child, ss, depth+1)
	}
}
