// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package webrtcproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func readAll(t *testing.T, r io.Reader) []*Event {
	t.Helper()
	rd := NewReader(r)
	var got []*Event
	for {
		ev, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		require.NoError(t, err)
		got = append(got, ev)
	}
}

func TestRoundTrip(t *testing.T) {
	events := []*Event{
		{Type: EventInit, Init: &Init{
			SampleRate:               16000,
			NumInputChannels:         1,
			NumOutputChannels:        1,
			NumReverseChannels:       1,
			ReverseSampleRate:        16000,
			OutputSampleRate:         16000,
			ReverseOutputSampleRate:  16000,
			NumReverseOutputChannels: 1,
			TimestampMS:              1234,
		}},
		{Type: EventConfig, Config: &ConfigEvent{AECEnabled: true, HPFEnabled: true, NSEnabled: true, NSLevel: 2}},
		{Type: EventReverseStream, ReverseStream: &ReverseStream{Channels: [][]float32{makeRamp(160)}}},
		{Type: EventStream, Stream: &Stream{
			DelayMS:            50,
			AppliedInputVolume: -3,
			InputChannels:      [][]float32{makeRamp(160)},
			OutputChannels:     [][]float32{makeRamp(160)},
		}},
	}
	var buf bytes.Buffer
	for _, ev := range events {
		require.NoError(t, WriteEvent(&buf, ev))
	}
	assert.Equal(t, events, readAll(t, &buf))
}

func TestEncode_Deterministic(t *testing.T) {
	ev := &Event{
		Type:   EventInit,
		Init:   &Init{SampleRate: 48000, NumInputChannels: 2, TimestampMS: 7},
		Config: &ConfigEvent{AECEnabled: true, AGCEnabled: true, NSEnabled: true},
	}
	assert.Equal(t, encodeEvent(ev), encodeEvent(ev))
}

// TestDecode_UpstreamFieldNumbers pins the field numbers to debug.proto.
func TestDecode_UpstreamFieldNumbers(t *testing.T) {
	var init []byte
	init = protowire.AppendTag(init, 9, protowire.VarintType) // num_reverse_output_channels
	init = protowire.AppendVarint(init, 2)
	init = protowire.AppendTag(init, 10, protowire.VarintType) // timestamp_ms
	init = protowire.AppendVarint(init, 99)

	var cfg []byte
	cfg = protowire.AppendTag(cfg, 13, protowire.VarintType) // hpf_enabled
	cfg = protowire.AppendVarint(cfg, 1)
	cfg = protowire.AppendTag(cfg, 15, protowire.VarintType) // ns_level
	cfg = protowire.AppendVarint(cfg, 3)

	msg := protowire.AppendTag(nil, 1, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(EventInit))
	msg = protowire.AppendTag(msg, 2, protowire.BytesType)
	msg = protowire.AppendBytes(msg, init)
	msg = protowire.AppendTag(msg, 5, protowire.BytesType)
	msg = protowire.AppendBytes(msg, cfg)

	ev, err := decodeEvent(msg)
	require.NoError(t, err)
	assert.Equal(t, int32(2), ev.Init.NumReverseOutputChannels)
	assert.Equal(t, int64(99), ev.Init.TimestampMS)
	assert.True(t, ev.Config.HPFEnabled)
	assert.Equal(t, int32(3), ev.Config.NSLevel)
}

func TestReader_SkipsUnknownFields(t *testing.T) {
	msg := protowire.AppendTag(nil, 1, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(EventStream))
	msg = protowire.AppendTag(msg, 99, protowire.BytesType)
	msg = protowire.AppendBytes(msg, []byte("junk"))

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, uint32(len(msg)))) //nolint:gosec // Small fixture.
	buf.Write(msg)
	require.NoError(t, WriteEvent(&buf, &Event{Type: EventInit, Init: &Init{SampleRate: 48000}}))

	got := readAll(t, &buf)
	require.Len(t, got, 2)
	assert.Equal(t, EventStream, got[0].Type)
	assert.Equal(t, int32(48000), got[1].Init.SampleRate)
}

func TestReader_Errors(t *testing.T) {
	_, err := NewReader(bytes.NewReader([]byte{0, 0, 0, 0})).Next()
	require.ErrorIs(t, err, errZeroLengthEvent)

	// Varint field truncated after its tag.
	_, err = NewReader(bytes.NewReader([]byte{1, 0, 0, 0, 0x08})).Next()
	require.ErrorIs(t, err, errMalformed)

	_, err = NewReader(bytes.NewReader([]byte{4, 0, 0, 0, 0x08})).Next()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func makeRamp(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(i) / float32(n)
	}

	return out
}
