# Listening corpus

A curated set of before/after WAV pairs reviewed by ear before any tuning
change ships. This is the *subjective* gate of the testing pipeline,
complementing the metric-driven gates in `test/property`, `test/aecdump`,
and `cmd/qa-runner`.

## When to update

Regenerate and audit these snapshots whenever you change a DSP coefficient
or threshold inside any module. Commit the new pair under a version
folder (`v0.1.0/`, `v0.1.1/`, ...). Old versions stay in the tree so
auditors can compare across releases.

## How to listen

1. Use closed-back headphones in a quiet room. Set output level to a
   comfortable speaking volume.
2. Open `manifest.yaml` and read the "what to listen for" note before
   each pair.
3. Listen to `<scenario>_before.wav`, then immediately `<scenario>_after.wav`.
4. If the *after* file regressed against the spec note, do **not** merge.

A 5-minute pass through the full set is the bar before any tuning change
lands. Capture impressions in the PR description ("kitchen echo cleaner;
café noise musical artifacts comparable").

## File naming

```
v<release>/<scenario>_before.wav   (input to APM)
v<release>/<scenario>_after.wav    (APM output)
```

Both files must be the same sample rate and channel count.

## Why not automate this?

Objective metrics catch big breakages but reliably miss subtle ones
(musicality, warble, residual echo that's quiet but audible) that human
listeners catch instantly. We make this step deliberately manual to keep
the human in the loop.
