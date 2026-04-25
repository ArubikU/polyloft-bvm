# HTML-like Counter Sample (html.go + CSS)

This sample shows a typed Go builder style for UI composition while CSS stays external.

It uses giocss as the UI/CSS engine primitives:

- Node tree via `giocss.Node`
- Stylesheet loading via `giocss.NewStyleSheet` and `LoadFile`
- Node style resolution via `giocss.ResolveNodeStyle`

## Run

From repository root:

```powershell
go run ./polyloft-bvm/samples/01-htmlgo-counter
```

The sample simulates interactions by rebuilding and re-rendering the node tree.
