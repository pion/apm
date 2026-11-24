// SPDX-FileCopyrightText: 2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Port of
// ConfigSelectorChannelsAndContentDetection.MonoConfigIsSelectedWhenNoMultiChannelConfigPresent.
func TestConfigSelector_MonoConfigIsSelectedWhenNoMultiChannelConfigPresent(t *testing.T) {
	for _, numChannels := range []int{1, 2, 8} {
		for _, detectStereoContent := range []bool{false, true} {
			config := defaultEchoCanceller3Config()
			config.multiChannel.detectStereoContent = detectStereoContent
			config.delay.defaultDelay++
			customDelayValueInConfig := config.delay.defaultDelay

			selector := newConfigSelector(config, nil, numChannels)
			assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)

			selector.update(false)
			assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)

			selector.update(true)
			assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)
		}
	}
}

// Port of
// ConfigSelectorChannelsAndContentDetection.CorrectInitialConfigIsSelected.
func TestConfigSelector_CorrectInitialConfigIsSelected(t *testing.T) {
	for _, numChannels := range []int{1, 2, 8} {
		for _, detectStereoContent := range []bool{false, true} {
			config := defaultEchoCanceller3Config()
			config.multiChannel.detectStereoContent = detectStereoContent
			multichannelConfig := config

			config.delay.defaultDelay++
			customDelayValueInConfig := config.delay.defaultDelay
			multichannelConfig.delay.defaultDelay += 2
			customDelayValueInMultichannelConfig := multichannelConfig.delay.defaultDelay

			selector := newConfigSelector(config, &multichannelConfig, numChannels)

			if numChannels == 1 || detectStereoContent {
				assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)
			} else {
				assert.Equal(t, customDelayValueInMultichannelConfig, selector.activeConfig().delay.defaultDelay)
			}
		}
	}
}

// Port of ConfigSelectorChannels.CorrectConfigUpdateBehavior.
func TestConfigSelector_CorrectConfigUpdateBehavior(t *testing.T) {
	for _, numChannels := range []int{1, 2, 8} {
		config := defaultEchoCanceller3Config()
		config.multiChannel.detectStereoContent = true
		multichannelConfig := config

		config.delay.defaultDelay++
		customDelayValueInConfig := config.delay.defaultDelay
		multichannelConfig.delay.defaultDelay += 2
		customDelayValueInMultichannelConfig := multichannelConfig.delay.defaultDelay

		selector := newConfigSelector(config, &multichannelConfig, numChannels)

		selector.update(false)
		assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)

		if numChannels == 1 {
			selector.update(false)
			assert.Equal(t, customDelayValueInConfig, selector.activeConfig().delay.defaultDelay)
		} else {
			selector.update(true)
			assert.Equal(t, customDelayValueInMultichannelConfig, selector.activeConfig().delay.defaultDelay)
		}
	}
}
