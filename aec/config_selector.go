// SPDX-FileCopyrightText: 2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// configSelector mirrors ConfigSelector: it selects the config to use,
// the mono one or the multichannel one.
type configSelector struct {
	config             echoCanceller3Config
	multichannelConfig *echoCanceller3Config
	active             *echoCanceller3Config
}

// newConfigSelector creates a selector. multichannelConfig is nil when
// there is no config for multichannel content.
func newConfigSelector(
	config echoCanceller3Config, multichannelConfig *echoCanceller3Config, numRenderInputChannels int,
) *configSelector {
	selector := &configSelector{config: config}
	if multichannelConfig != nil {
		copied := *multichannelConfig
		selector.multichannelConfig = &copied
	}
	selector.update(!config.multiChannel.detectStereoContent && numRenderInputChannels > 1)

	return selector
}

// update mirrors Update: it updates the config selection based on the
// detection of multichannel content.
func (c *configSelector) update(multichannelContent bool) {
	if multichannelContent && c.multichannelConfig != nil {
		c.active = c.multichannelConfig
	} else {
		c.active = &c.config
	}
}

// activeConfig mirrors active_config.
func (c *configSelector) activeConfig() *echoCanceller3Config { return c.active }
