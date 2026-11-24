# .aecdump corpus

Real `.aecdump` recordings for `test/aecdump`. The upstream WebRTC tool
`audioproc_f` writes them with `--dump-output`.

For each scenario, add the dump (`<scenario>.aecdump`) and its pass/fail
bounds in `../thresholds.yaml`.

Planned scenarios:

* `echo_easy`: single talker, low reverb, no near-end speech
* `echo_doubletalk`: overlapping near and far speech, moderate room
* `ns_noisy_room`: speech and cafe noise, no echo
* `agc_quiet_to_loud`: single talker with a level step

Only commit dumps recorded from material we may redistribute (CC-BY or
CC0 corpora, or speech recorded with contributor consent).
