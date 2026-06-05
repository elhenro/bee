# Token-driven procedural generating animation

## Goal

Replace the default "generating" loader with a procedurally generated
particle-stream gauge that visualizes live turn progress using the actual
input/output token figures bee already tracks.

## Scope

- Replaces only the **default** loader (the phased `braillePhaseFor`
  painter). The 28 named `BEE_LOADER` styles stay pinnable.
- Persistent strip (option B): a dedicated one-row gauge rendered for the
  whole turn (waiting + streaming, across tool calls), placed below the
  streaming text in the live region.

## Data sources (no token estimation)

- **input** — `costs.LastInput()`, the real context-token count sent.
  Rendered `in 12.3k tok`. 0 on the first turn until the first provider
  event lands.
- **output** — live cumulative char count for the turn (`turnOutChars`),
  incremented per text + reasoning delta. Rendered `out 1,847 ch`. The
  `ch` unit signals a live estimate; the real output-token tally already
  surfaces at turn end via the cost flash. No fabricated token math.
- **rate** — chars produced since the previous frame, sampled per
  `loaderTickMsg`. Drives particle emission density: idle wait drifts
  sparse, fast generation packs dense.

## Procedural generation

Per-turn random seed rolled on submit. Seed varies particle phase
offsets, vertical lanes, trail length, and accent shimmer cadence so every
generation looks distinct. Rendering reuses the existing drawille canvas +
braille pipeline (`NewDrawilleCanvas`, `SetPixel`, `ToBraille`).

## Row format

```
⬢ ⠠⠂⠐⠈⠂⠠⠐⠂⠈⠠⠂⠐  in 12.3k tok · out 1.8k ch
```

Particle stream fills the left; readout right-aligned. Width-aware — narrow
terminals drop the `tok`/`ch` units and the `in` label, collapsing toward
`out 1.8k`. Below `brailleLoaderMinCells` of room, readout is omitted and
only the stream renders.

## Components

- `braille_token_stream.go` *(new)* — `LoaderStats` struct, seeded particle
  painter `renderTokenStream(stats, frame, cells)`, readout formatter.
- `model.go` — fields `turnOutChars`, `loaderSeed`, `loaderLastSample`
  (chars + frame for rate).
- `app_keys_submit.go` — reset counters and roll `loaderSeed` on submit.
- `app_update_stream.go` — bump `turnOutChars` in `onStreamDelta` and
  `onThinkDelta`.
- `stream_loader.go` — default painter routes to the token stream; plumb
  `LoaderStats` into the renderer.
- `view.go` — render the persistent strip during the turn, feed it stats.
- `braille_token_stream_test.go` *(new)* — exact-width output, seed
  determinism, readout formatting + collapse.

## Error / edge handling

- Zero input (first turn) renders `in — tok` (no fake number).
- Zero output (pre-token wait) renders sparse drift, `out 0 ch`.
- Rate sampler guards against negative deltas (partial reset on tool
  boundary) — clamp to 0, never panic.
- Strip respects compact mode (no outer gutter) and `showLoader=false`
  (static `⬢` row, no animation, numbers still shown).

## Testing

Painter output is deterministic given (seed, frame, cells): assert exact
rune width, stable output for a fixed seed, divergence across seeds, and
readout string formatting at several widths.
