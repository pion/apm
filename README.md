<h1 align="center">
  <br>
  Pion APM
  <br>
</h1>
<h4 align="center">Audio processing (echo cancellation, noise suppression, gain control) in pure Go</h4>
<p align="center">
  <a href="https://pion.ly"><img src="https://img.shields.io/badge/pion-apm-gray.svg?longCache=true&colorB=brightgreen" alt="Pion APM"></a>
  <a href="https://discord.gg/PngbdqpFbt"><img src="https://img.shields.io/badge/join-us%20on%20discord-gray.svg?longCache=true&logo=discord&colorB=brightblue" alt="join us on Discord"></a> <a href="https://bsky.app/profile/pion.ly"><img src="https://img.shields.io/badge/follow-us%20on%20bluesky-gray.svg?longCache=true&logo=bluesky&colorB=brightblue" alt="Follow us on Bluesky"></a>  <br>
  <img alt="GitHub Workflow Status" src="https://img.shields.io/github/actions/workflow/status/pion/apm/test.yaml">
  <a href="https://pkg.go.dev/github.com/pion/apm"><img src="https://pkg.go.dev/badge/github.com/pion/apm.svg" alt="Go Reference"></a>
  <a href="https://codecov.io/gh/pion/apm"><img src="https://codecov.io/gh/pion/apm/branch/master/graph/badge.svg" alt="Coverage Status"></a>
  <a href="https://goreportcard.com/report/github.com/pion/apm"><img src="https://goreportcard.com/badge/github.com/pion/apm" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>
<br>

A pure Go port of the WebRTC Audio Processing Module: echo cancellation (AEC3),
noise suppression, automatic gain control (AGC2) and high-pass filtering.

### Usage
```go
cfg := apm.DefaultConfig(apm.Rate48k, 1)
cfg.AEC.Enabled = true
cfg.NS = apm.NSConfig{Enabled: true, Level: apm.NSModerate}

proc, err := apm.New(cfg)
if err != nil {
	return err
}

// For every 10 ms frame:
_ = proc.ProcessReverseStream(farEnd) // audio sent to the speaker
_ = proc.ProcessStream(nearEnd)       // microphone audio, processed in place
```

Supported rates are 8, 16, 32 and 48 kHz. An 8 kHz stream is resampled to
16 kHz for processing. As in WebRTC, the high-pass filter, noise suppressor
and echo canceller run at 32 kHz at most by default, so a 48 kHz stream is
resampled to 32 kHz for them; set `MaxProcessingRate` to `apm.Rate48k` to
process the full band. Above 16 kHz the echo canceller only works on the
lowest 16 kHz band.

### Roadmap
The library is used as a part of our WebRTC implementation. Please refer to that [roadmap](https://github.com/pion/webrtc/issues/9) to track our major milestones.

### Community
Pion has an active community on the [Discord](https://discord.gg/PngbdqpFbt).

Follow the [Pion Bluesky](https://bsky.app/profile/pion.ly) or [Pion Twitter](https://twitter.com/_pion) for project updates and important WebRTC news.

We are always looking to support **your projects**. Please reach out if you have something to build!
If you need commercial support or don't want to use public methods you can contact us at [team@pion.ly](mailto:team@pion.ly)

### Contributing
Check out the [contributing wiki](https://github.com/pion/webrtc/wiki/Contributing) to join the group of amazing people making this project possible

### License
MIT License - see [LICENSE](LICENSE) for full text

Files ported from [WebRTC](https://webrtc.googlesource.com/src) are also under the
[BSD 3-Clause License](LICENSES/BSD-3-Clause.txt).
