// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// echoAudibility mirrors EchoAudibility: it tells in which bands the
// render signal is stationary, where the echo is not audible.
type echoAudibility struct {
	renderSpectrumWritePrev     int
	hasRenderSpectrumWritePrev  bool
	renderBlockWritePrev        int
	nonZeroRenderSeen           bool
	useRenderStationarityAtInit bool
	renderStationarity          stationarityEstimator
}

func newEchoAudibility(useRenderStationarityAtInit bool) *echoAudibility {
	return &echoAudibility{
		useRenderStationarityAtInit: useRenderStationarityAtInit,
		renderStationarity:          newStationarityEstimator(),
	}
}

// update mirrors Update: it feeds new render data to the estimator.
func (e *echoAudibility) update(
	render *renderBuffer, averageReverb *[kFftLengthBy2Plus1]float32, delayBlocks int, externalDelaySeen bool,
) {
	e.updateRenderNoiseEstimator(render.getSpectrumBuffer(), render.getBlockBuffer(), externalDelaySeen)

	if externalDelaySeen || e.useRenderStationarityAtInit {
		e.updateRenderStationarityFlags(render, averageReverb, delayBlocks)
	}
}

// getResidualEchoScaling mirrors GetResidualEchoScaling.
func (e *echoAudibility) getResidualEchoScaling(filterHasHadTimeToConverge bool, residualScaling []float32) {
	for band := range residualScaling {
		if e.renderStationarity.isBandStationary(band) &&
			(filterHasHadTimeToConverge || e.useRenderStationarityAtInit) {
			residualScaling[band] = 0
		} else {
			residualScaling[band] = 1
		}
	}
}

// isBlockStationary mirrors IsBlockStationary: whether the current render
// block is estimated as stationary.
func (e *echoAudibility) isBlockStationary() bool {
	return e.renderStationarity.isBlockStationary()
}

// updateRenderStationarityFlags mirrors UpdateRenderStationarityFlags.
func (e *echoAudibility) updateRenderStationarityFlags(
	render *renderBuffer, averageReverb *[kFftLengthBy2Plus1]float32, minChannelDelayBlocks int,
) {
	spectrumBuf := render.getSpectrumBuffer()
	idxAtDelay := spectrumBuf.offsetIndex(spectrumBuf.read, minChannelDelayBlocks)

	numLookahead := max(0, render.headroom()-minChannelDelayBlocks+1)

	e.renderStationarity.updateStationarityFlags(spectrumBuf, averageReverb, idxAtDelay, numLookahead)
}

// updateRenderNoiseEstimator mirrors UpdateRenderNoiseEstimator: it
// updates the noise estimator with the render data since the previous
// call.
func (e *echoAudibility) updateRenderNoiseEstimator(
	spectrumBuf *spectrumBuffer, blockBuf *blockBuffer, externalDelaySeen bool,
) {
	if !e.hasRenderSpectrumWritePrev {
		e.renderSpectrumWritePrev = spectrumBuf.write
		e.hasRenderSpectrumWritePrev = true
		e.renderBlockWritePrev = blockBuf.write

		return
	}
	renderSpectrumWriteCurrent := spectrumBuf.write
	if !e.nonZeroRenderSeen && !externalDelaySeen {
		e.nonZeroRenderSeen = !e.isRenderTooLow(blockBuf)
	}
	if e.nonZeroRenderSeen {
		for idx := e.renderSpectrumWritePrev; idx != renderSpectrumWriteCurrent; idx = spectrumBuf.decIndex(idx) {
			e.renderStationarity.updateNoiseEstimator(spectrumBuf.buffer[idx])
		}
	}
	e.renderSpectrumWritePrev = renderSpectrumWriteCurrent
}

// isRenderTooLow mirrors IsRenderTooLow: whether the render signal only
// holds values close to zero.
func (e *echoAudibility) isRenderTooLow(blockBuf *blockBuffer) bool {
	tooLow := false
	renderBlockWriteCurrent := blockBuf.write
	if renderBlockWriteCurrent == e.renderBlockWritePrev {
		tooLow = true
	} else {
		for idx := e.renderBlockWritePrev; idx != renderBlockWriteCurrent; idx = blockBuf.incIndex(idx) {
			var maxAbsOverChannels float32
			for ch := range blockBuf.buffer[idx].numChannels {
				maxAbsOverChannels = max(maxAbsOverChannels, maxAbs(blockBuf.buffer[idx].view(0, ch)))
			}
			if maxAbsOverChannels < 10 {
				// Discards all blocks if one of them is too low.
				tooLow = true

				break
			}
		}
	}
	e.renderBlockWritePrev = renderBlockWriteCurrent

	return tooLow
}
