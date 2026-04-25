# Giocss extraction status

## Goal

Move CSS and Gio style resolution out of polyloft-bvm into a standalone module:

- Module: github.com/ArubikU/giocss
- Publish target: GitHub public repository + Go proxy

## Implemented in this phase

- Created new Go module at ../../giocss.
- Ported CSS core engine into giocss/stylesheet.go.
- Ported HTML-like node model and layout helpers into giocss/dom.go.
- Ported shared CSS/render helpers into giocss/helpers.go.
- Ported input normalization and cursor mapping into giocss/input_cursor.go.
- Ported transition/transform/filter parsing into giocss/transition.go.
- Ported render/event engine helpers into giocss/engine_helpers.go.
- Ported scrollbar and scroll event logic into giocss/scroll_logic.go.
- Ported scrollbar render-data computation into giocss/scroll_render.go.
- Ported path interaction state helpers into giocss/interaction_state.go.
- Ported media/image loading and CSS image filter pipeline into giocss/media.go.
- Added OO render transition store in giocss/render_store.go.
- Ported profiling/cache helpers into giocss/profiling_helpers.go.
- Ported profiler dump/overlay/debug component services into giocss/debug_profile.go.
- Ported picker modal layout and visual model into giocss/picker_modal.go.
- Ported global renderer z-index/out-of-flow heuristics into giocss/engine_helpers.go.
- Ported global renderer position/overflow layout calculators into giocss/renderer_layout.go.
- Ported global renderer visibility/culling calculator into giocss/renderer_layout.go.
- Ported global renderer child render planning (z-order and out-of-flow detection) into giocss/renderer_children.go.
- Ported global renderer z-sort hint cache resolution into giocss/renderer_children.go.
- Ported global renderer child traversal decision planning into giocss/renderer_children.go.
- Ported global renderer children content-bounds computation into giocss/renderer_children.go.
- Ported drawGioTree text/button content planning into giocss/renderer_text.go.
- Ported drawGioTree input/native geometry planners into giocss/renderer_input.go.
- Ported drawGioTree checkbox/radio/select state resolution planners into giocss/renderer_input.go.
- Ported drawGioTree slider min/max/init/clamp state resolution planners into giocss/renderer_input.go.
- Ported drawGioTree slider pointer-to-value mapping helper into giocss/renderer_input.go.
- Ported drawGioTree input editor config/sync policy helpers into giocss/renderer_input.go.
- Ported drawGioTree single-line input horizontal scroll/caret layout planner into giocss/renderer_input.go.
- Ported drawGioTree input scroll-indicator geometry planner into giocss/renderer_input.go.
- Ported drawGioTree input spinner/picker geometry planners into giocss/renderer_input.go.
- Ported drawGioTree native progress visual fill planner into giocss/renderer_input.go.
- Ported drawGioTree shared checkbox/radio geometry planner into giocss/renderer_input.go.
- Ported drawGioTree native textarea editor setup/sync policy reuse via giocss input helpers.
- Ported drawGioTree input color swatch geometry planner into giocss/renderer_input.go.
- Ported drawGioTree native textarea editor geometry planner into giocss/renderer_input.go.
- Ported global renderer event-area decision helper into giocss/renderer_layout.go.
- Ported global renderer content-bounds scroll clamp helper into giocss/scroll_logic.go.
- Ported drawGioTree quick pre-cull decision into giocss/renderer_layout.go.
- Ported drawGioTree display/visibility and filter-factor planning helpers into giocss/renderer_layout.go.
- Ported drawGioTree orchestration helpers (profile decision, cursor policy, child traversal plan) into giocss/renderer_layout.go.
- Exported core API in giocss:
  - NewStyleSheet
  - SetRule
  - GetRule
  - GetClassProps
  - ParseCSSText
  - ResolveStyle
  - CanonicalName
  - CSSLengthValue
  - CSSFloatValue
- Replaced duplicated runtime helper implementations in polyloft-bvm with wrappers:
  - internal/runtime/ui_css.go -> delegates to giocss stylesheet APIs
  - internal/runtime/ui_gio_render.go -> delegates input/cursor utilities
  - internal/runtime/ui_transition_helpers.go -> delegates transition/filter/position parsers
  - internal/runtime/ui_style_helpers.go -> delegates color/text/measurement helpers
  - internal/runtime/ui_gio_render.go -> delegates engine helpers (viewport/render rect, point hit, bool-map refresh, inherited text signature)
  - internal/runtime/ui_gio_render.go -> delegates scrollbar math and pointer scroll accumulation
  - internal/runtime/ui_gio_render.go -> delegates scrollbar metrics/geometry and only draws tracks/thumbs in BVM
  - internal/runtime/ui_style_helpers.go -> delegates hover/active path state map mutations
  - internal/runtime/ui_media_helpers.go -> bridge-only wrappers to giocss for image loading/filtering/filter-chain parsing
  - internal/runtime/ui_transition_helpers.go -> now uses giocss.RenderStore for frame transition state
  - internal/runtime/ui_gio_render.go -> delegates scroll target selection and profiling helper logic to giocss
  - internal/runtime/ui_gio_render.go -> delegates profiler dump writer and profiler overlay line composition to giocss
  - internal/runtime/ui_gio_render.go -> delegates debug component stat upsert and uses giocss debug stat type alias
  - internal/runtime/ui_gio_render.go -> delegates debug overlay policy/model decisions to giocss
  - internal/runtime/ui_gio_render.go -> delegates picker modal geometry/colors/paths model to giocss
  - internal/runtime/ui_gio_render.go -> delegates z-index and out-of-flow detection helpers to giocss
  - internal/runtime/ui_gio_render.go -> delegates position offsets and overflow clip mode resolution to giocss
  - internal/runtime/ui_gio_render.go -> delegates viewport/renderRect visibility and culling checks to giocss
  - internal/runtime/ui_gio_render.go -> delegates child z-order planning and out-of-flow child detection to giocss
  - internal/runtime/ui_gio_render.go -> delegates z-sort hint cache resolution to giocss
  - internal/runtime/ui_gio_render.go -> delegates child traversal decision flow to giocss
  - internal/runtime/ui_gio_render.go -> delegates children content-bounds computation to giocss
  - internal/runtime/ui_gio_render.go -> delegates text/button content planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates input box metrics, picker positioning and slider visual planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates checkbox/radio/select state model resolution to giocss
  - internal/runtime/ui_gio_render.go -> delegates slider state model resolution (min/max/init/clamp) to giocss
  - internal/runtime/ui_gio_render.go -> delegates slider pointer-to-value calculation to giocss
  - internal/runtime/ui_gio_render.go -> delegates input editor setup/sync policy decisions to giocss
  - internal/runtime/ui_gio_render.go -> delegates single-line input horizontal scroll planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates input scroll-indicator geometry planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates input spinner/picker geometry planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates native progress fill planning to giocss
  - internal/runtime/ui_gio_render.go -> delegates checkbox/radio geometry planning to giocss across input/native branches
  - internal/runtime/ui_gio_render.go -> reuses giocss editor setup/sync policy helpers for native textarea
  - internal/runtime/ui_gio_render.go -> delegates input color swatch geometry and native textarea layout geometry to giocss

## Runtime componentization progress

- Extracted types/state declarations from `internal/runtime/ui_gio_render.go` into `internal/runtime/ui_gio_render_types.go`.
- Extracted window render-state lifecycle helpers into `internal/runtime/ui_gio_render_logic_state.go`.
- Consolidated window loop UI host orchestration (`openGioWindow`) into `internal/runtime/ui_gio_render.go` and removed `internal/runtime/ui_gio_render_ui_window_loop.go`.
- Consolidated picker modal rendering into `internal/runtime/ui_gio_render.go` and removed `internal/runtime/ui_gio_render_ui_picker_modal.go`.
- Extracted shared input/native event-handler policy (checkbox/radio/slider/select payload and dispatch flow) into `internal/runtime/ui_gio_render_logic_input_events.go` and rewired `drawGioTree` to use those helpers.
- Ported input event-policy helpers (focusable-path normalization, input-kind detection, event-name resolution, spinner/picker path and payload builders) into `giocss/renderer_events.go` and rewired runtime pointer/input dispatch flows to consume them.
- Ported box-shadow utility pipeline (layer parsing/cache, shadow raster generation/cache, shadow template generation/cache) into `giocss/renderer_shadow.go`; runtime keeps Gio draw calls and render-tree orchestration as a thin adapter.
- Ported box-shadow nine-slice geometry planning for template shadows into `giocss/renderer_shadow.go`; runtime keeps only the Gio raster-slice draw loop.
- Simplified runtime shadow adapters in `internal/runtime/ui_gio_render.go` to use direct type aliases (`BoxShadowLayer`, `ShadowRaster`, `ShadowTemplate`) from `giocss` and remove field-by-field conversion glue.
- Ported background/gradient utility pipeline (background layer parsing/cache, background filter-factor parsing, gradient stop parsing/cache, gradient raster cache keys, angle/direction/center parsers, and gradient palette blending helpers) into `giocss/renderer_gradient.go`; runtime keeps Gio draw orchestration and paint ops as a thin adapter.
- Ported border-radius and rounded-value utility pipeline (CSS border-radius longhand/shorthand parsing, effective border radius resolution, and rounded/radius props fallback resolution) into `giocss/renderer_border_radius.go`; runtime keeps border/background draw ops and render-tree orchestration as a thin adapter.
- Ported debug/profiling utility flow (component metric update builder, profiler flag normalization/list extraction, component-stat snapshot copy, and profile-dump data builder) into `giocss/debug_profile.go`; runtime keeps profiler orchestration, window state ownership, and Gio overlay draw ops.
- Ported text-decoration and debug-outline geometry planners into `giocss/renderer_paint_plans.go` (dotted/dashed/solid decoration segments, text decoration placement planning, and debug outline rectangle planning); runtime keeps only Gio paint ops.
- Ported per-side border parsing/normalization planner into `giocss/renderer_border_plan.go`; runtime keeps only border paint ops and rounded clipping.
- Ported dashed/dotted border segment planning and image paint transform planning into `giocss/renderer_paint_plans.go`; runtime keeps only Gio clip/paint execution.
- Ported input event-state mutation helpers (`checkbox`, `radio`, `slider pointer`, `select cycle`) into `giocss/runtime_event_state.go`; runtime keeps only dispatch/invalidate bridge closures.
- Removed `internal/runtime/ui_gio_render_logic_state.go` and consolidated its state lifecycle helpers into `internal/runtime/ui_gio_render_types.go`.
- Removed `internal/runtime/ui_gio_render_logic_input_events.go` and consolidated handler registration bridge in `internal/runtime/ui_gio_render.go`.
  - internal/runtime/ui_gio_render.go -> delegates node event-area registration decision to giocss
  - internal/runtime/ui_gio_render.go -> delegates scroll offset clamp-to-content to giocss
  - internal/runtime/ui_gio_render.go -> delegates quick offscreen pre-cull check to giocss
  - internal/runtime/ui_gio_render.go -> delegates display-none, visibility-hidden and filter-factor mapping decisions to giocss
  - internal/runtime/ui_gio_render.go -> delegates profile/cursor/child-traversal planning decisions to giocss
- Added dependency wiring in polyloft-bvm/go.mod:
  - require github.com/ArubikU/giocss v0.0.0
  - replace github.com/ArubikU/giocss => ../giocss

## Validation

- go test ./... in giocss: pass
- go test ./internal/runtime in polyloft-bvm: pass

## Next implementation cuts

1. Extract remaining Gio render pipeline from internal/runtime/ui_gio_render.go into giocss engine package.
2. Move window interaction state (hover/active/focus/scroll/editor maps) into giocss engine lifecycle.
3. Keep polyloft-bvm runtime as thin adapter for component props and event bridge callbacks.
4. Add parity tests for render scenes (background/border/shadow/text/scroll/focus) across old vs new path.
5. Expand samples in polyloft-bvm/samples (forms, list, todo, modal, animation) using shared CSS.
6. Publish giocss v0.1.0 and replace local go.mod replace with tagged version.
