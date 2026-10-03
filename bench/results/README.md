# Raw benchmark results

`run-<id>/` holds the raw per-run samples (`results_<platform>_<replicate>.json`),
the console log of every CI job, and the LaTeX tables generated from them by
`scripts/make_tables.py`. They were produced by `.github/workflows/bench.yml`
(GitHub Actions run `<id>`, Go 1.25.3, CPython 3.12.10 unless the platform label says
otherwise). Replicates of one platform may land on different CPUs; each JSON records
the CPU model reported by the runner.

Regenerate the tables:

    python scripts/make_tables.py "bench/results/run-<id>/results_*.json" --out tables.tex --outdir tables/
