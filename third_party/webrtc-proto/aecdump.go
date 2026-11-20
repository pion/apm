// SPDX-FileCopyrightText: 2011 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package webrtcproto reads and writes WebRTC .aecdump files, as described
// in debug.proto. Only the fields needed to replay a dump through the APM
// are decoded; unknown fields are skipped.
package webrtcproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"

	"google.golang.org/protobuf/encoding/protowire"
)

var (
	errZeroLengthEvent = errors.New("aecdump: zero-length event")
	errEventTooLarge   = errors.New("aecdump: event too large")
	errMalformed       = errors.New("aecdump: malformed field")
)

// EventType mirrors the Event.Type enum in debug.proto.
type EventType int32

// Event types.
const (
	EventInit           EventType = 0
	EventReverseStream  EventType = 1
	EventStream         EventType = 2
	EventConfig         EventType = 3
	EventUnknown        EventType = 4
	EventRuntimeSetting EventType = 5
)

func (t EventType) String() string {
	switch t {
	case EventInit:
		return "INIT"
	case EventReverseStream:
		return "REVERSE_STREAM"
	case EventStream:
		return "STREAM"
	case EventConfig:
		return "CONFIG"
	case EventUnknown:
		return "UNKNOWN_EVENT"
	case EventRuntimeSetting:
		return "RUNTIME_SETTING"
	default:
		return fmt.Sprintf("EVENT(%d)", int32(t))
	}
}

// Init is the Init message: stream formats, sent once at the start of a dump.
type Init struct {
	SampleRate               int32
	NumInputChannels         int32
	NumOutputChannels        int32
	NumReverseChannels       int32
	ReverseSampleRate        int32
	OutputSampleRate         int32
	ReverseOutputSampleRate  int32
	NumReverseOutputChannels int32
	TimestampMS              int64
}

// ReverseStream is one far-end frame.
type ReverseStream struct {
	// Channels holds deinterleaved float audio. Old dumps store interleaved
	// int16 in LegacyData instead.
	Channels   [][]float32
	LegacyData []byte
}

// Stream is one near-end frame.
type Stream struct {
	InputChannels      [][]float32
	OutputChannels     [][]float32
	LegacyInputData    []byte
	LegacyOutputData   []byte
	DelayMS            int32
	AppliedInputVolume int32
}

// ConfigEvent is the subset of the Config message used for replay.
type ConfigEvent struct {
	AECEnabled bool
	AGCEnabled bool
	HPFEnabled bool
	NSEnabled  bool
	NSLevel    int32
}

// Event is one dump entry. The pointer field matching Type is set.
type Event struct {
	Type          EventType
	Init          *Init
	ReverseStream *ReverseStream
	Stream        *Stream
	Config        *ConfigEvent
}

// Field numbers from debug.proto.
const (
	fieldEventType          protowire.Number = 1
	fieldEventInit          protowire.Number = 2
	fieldEventReverseStream protowire.Number = 3
	fieldEventStream        protowire.Number = 4
	fieldEventConfig        protowire.Number = 5

	fieldInitTimestampMS protowire.Number = 10

	fieldReverseData    protowire.Number = 1
	fieldReverseChannel protowire.Number = 2

	fieldStreamInputData     protowire.Number = 1
	fieldStreamOutputData    protowire.Number = 2
	fieldStreamDelay         protowire.Number = 3
	fieldStreamAppliedVolume protowire.Number = 5
	fieldStreamInputChannel  protowire.Number = 7
	fieldStreamOutputChannel protowire.Number = 8

	fieldConfigAEC     protowire.Number = 1
	fieldConfigAGC     protowire.Number = 9
	fieldConfigHPF     protowire.Number = 13
	fieldConfigNS      protowire.Number = 14
	fieldConfigNSLevel protowire.Number = 15
)

// initInt32Fields maps Init field numbers to their int32 destinations.
func initInt32Fields(in *Init) map[protowire.Number]*int32 {
	return map[protowire.Number]*int32{
		1: &in.SampleRate,
		3: &in.NumInputChannels,
		4: &in.NumOutputChannels,
		5: &in.NumReverseChannels,
		6: &in.ReverseSampleRate,
		7: &in.OutputSampleRate,
		8: &in.ReverseOutputSampleRate,
		9: &in.NumReverseOutputChannels,
	}
}

// configBoolFields maps Config field numbers to their bool destinations.
func configBoolFields(c *ConfigEvent) map[protowire.Number]*bool {
	return map[protowire.Number]*bool{
		fieldConfigAEC: &c.AECEnabled,
		fieldConfigAGC: &c.AGCEnabled,
		fieldConfigHPF: &c.HPFEnabled,
		fieldConfigNS:  &c.NSEnabled,
	}
}

// Reader streams Events from an .aecdump file: each event is a 4-byte
// little-endian length followed by a serialized Event message.
type Reader struct {
	r   io.Reader
	buf []byte
}

// NewReader returns a Reader on r. The caller closes r.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

// Next reads one event. It returns io.EOF at the end of the stream.
func (rd *Reader) Next() (*Event, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(rd.r, hdr[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(hdr[:])
	if size == 0 {
		return nil, errZeroLengthEvent
	}
	if cap(rd.buf) < int(size) {
		rd.buf = make([]byte, size)
	}
	rd.buf = rd.buf[:size]
	if _, err := io.ReadFull(rd.r, rd.buf); err != nil {
		return nil, fmt.Errorf("aecdump: short event payload: %w", err)
	}

	return decodeEvent(rd.buf)
}

// field is one decoded protobuf field. Only the value matching its wire
// type is set.
type field struct {
	num     protowire.Number
	varint  uint64
	fixed32 uint32
	bytes   []byte
}

// int32 decodes an int32 varint. Negative values are sign-extended to 64
// bits on the wire, so truncating is the correct decoding.
func (f field) int32() int32 { return int32(f.varint) } //nolint:gosec // Truncation is the protobuf int32 decoding.

// forEachField calls fn for every field in the message b.
func forEachField(b []byte, fn func(f field) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return fmt.Errorf("%w: %w", errMalformed, protowire.ParseError(n))
		}
		b = b[n:]
		fld := field{num: num}
		switch typ {
		case protowire.VarintType:
			fld.varint, n = protowire.ConsumeVarint(b)
		case protowire.Fixed32Type:
			fld.fixed32, n = protowire.ConsumeFixed32(b)
		case protowire.BytesType:
			fld.bytes, n = protowire.ConsumeBytes(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return fmt.Errorf("%w %d: %w", errMalformed, num, protowire.ParseError(n))
		}
		b = b[n:]
		if err := fn(fld); err != nil {
			return err
		}
	}

	return nil
}

func decodeEvent(b []byte) (*Event, error) {
	event := &Event{Type: EventUnknown}
	err := forEachField(b, func(fld field) error {
		var err error
		switch fld.num {
		case fieldEventType:
			event.Type = EventType(fld.int32())
		case fieldEventInit:
			event.Init, err = decodeInit(fld.bytes)
		case fieldEventReverseStream:
			event.ReverseStream, err = decodeReverseStream(fld.bytes)
		case fieldEventStream:
			event.Stream, err = decodeStream(fld.bytes)
		case fieldEventConfig:
			event.Config, err = decodeConfig(fld.bytes)
		default:
			// Unknown fields are skipped.
		}

		return err
	})
	if err != nil {
		return nil, err
	}

	return event, nil
}

func decodeInit(b []byte) (*Init, error) {
	out := &Init{}
	int32s := initInt32Fields(out)
	err := forEachField(b, func(fld field) error {
		if dst, ok := int32s[fld.num]; ok {
			*dst = fld.int32()
		} else if fld.num == fieldInitTimestampMS {
			out.TimestampMS = int64(fld.varint) //nolint:gosec // Two's complement is the protobuf int64 decoding.
		}

		return nil
	})

	return out, err
}

func decodeReverseStream(b []byte) (*ReverseStream, error) {
	out := &ReverseStream{}
	err := forEachField(b, func(fld field) error {
		switch fld.num {
		case fieldReverseData:
			out.LegacyData = append([]byte(nil), fld.bytes...)
		case fieldReverseChannel:
			out.Channels = append(out.Channels, bytesToFloat32(fld.bytes))
		default:
			// Unknown fields are skipped.
		}

		return nil
	})

	return out, err
}

func decodeStream(b []byte) (*Stream, error) {
	out := &Stream{}
	err := forEachField(b, func(fld field) error {
		switch fld.num {
		case fieldStreamInputData:
			out.LegacyInputData = append([]byte(nil), fld.bytes...)
		case fieldStreamOutputData:
			out.LegacyOutputData = append([]byte(nil), fld.bytes...)
		case fieldStreamDelay:
			out.DelayMS = fld.int32()
		case fieldStreamAppliedVolume:
			out.AppliedInputVolume = fld.int32()
		case fieldStreamInputChannel:
			out.InputChannels = append(out.InputChannels, bytesToFloat32(fld.bytes))
		case fieldStreamOutputChannel:
			out.OutputChannels = append(out.OutputChannels, bytesToFloat32(fld.bytes))
		default:
			// Unknown fields are skipped.
		}

		return nil
	})

	return out, err
}

func decodeConfig(b []byte) (*ConfigEvent, error) {
	out := &ConfigEvent{}
	bools := configBoolFields(out)
	err := forEachField(b, func(fld field) error {
		if dst, ok := bools[fld.num]; ok {
			*dst = fld.varint != 0
		} else if fld.num == fieldConfigNSLevel {
			out.NSLevel = fld.int32()
		}

		return nil
	})

	return out, err
}

// bytesToFloat32 decodes a little-endian float32 array.
func bytesToFloat32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}

	return out
}

// WriteEvent writes ev to w with its length prefix. Only the fields the
// Reader decodes are written. Tests use it to build small fixtures.
func WriteEvent(w io.Writer, ev *Event) error {
	payload := encodeEvent(ev)
	if uint64(len(payload)) > math.MaxUint32 {
		return errEventTooLarge
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload))) //nolint:gosec // Bounded by the check above.
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)

	return err
}

// appendInt32 appends a non-zero int32 varint field.
func appendInt32(b []byte, num protowire.Number, v int32) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)

	return protowire.AppendVarint(b, uint64(int64(v))) //nolint:gosec // Sign extension is the protobuf int32 encoding.
}

// appendBool appends a true bool field.
func appendBool(b []byte, num protowire.Number, v bool) []byte {
	if !v {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.VarintType)

	return protowire.AppendVarint(b, 1)
}

func appendBytes(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)

	return protowire.AppendBytes(b, v)
}

func encodeEvent(ev *Event) []byte {
	b := protowire.AppendTag(nil, fieldEventType, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(ev.Type)) //nolint:gosec // Enum values are small and non-negative.
	if ev.Init != nil {
		b = appendBytes(b, fieldEventInit, encodeInit(ev.Init))
	}
	if ev.ReverseStream != nil {
		b = appendBytes(b, fieldEventReverseStream, encodeReverseStream(ev.ReverseStream))
	}
	if ev.Stream != nil {
		b = appendBytes(b, fieldEventStream, encodeStream(ev.Stream))
	}
	if ev.Config != nil {
		b = appendBytes(b, fieldEventConfig, encodeConfig(ev.Config))
	}

	return b
}

func encodeInit(in *Init) []byte {
	var b []byte
	fields := initInt32Fields(in)
	for _, num := range slices.Sorted(maps.Keys(fields)) {
		b = appendInt32(b, num, *fields[num])
	}
	if in.TimestampMS != 0 {
		b = protowire.AppendTag(b, fieldInitTimestampMS, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(in.TimestampMS)) //nolint:gosec // Two's complement is the protobuf int64 encoding.
	}

	return b
}

func encodeReverseStream(rs *ReverseStream) []byte {
	var b []byte
	for _, ch := range rs.Channels {
		b = appendBytes(b, fieldReverseChannel, float32sToBytes(ch))
	}

	return b
}

func encodeStream(stream *Stream) []byte {
	b := appendInt32(nil, fieldStreamDelay, stream.DelayMS)
	b = appendInt32(b, fieldStreamAppliedVolume, stream.AppliedInputVolume)
	for _, ch := range stream.InputChannels {
		b = appendBytes(b, fieldStreamInputChannel, float32sToBytes(ch))
	}
	for _, ch := range stream.OutputChannels {
		b = appendBytes(b, fieldStreamOutputChannel, float32sToBytes(ch))
	}

	return b
}

func encodeConfig(cfg *ConfigEvent) []byte {
	var b []byte
	fields := configBoolFields(cfg)
	for _, num := range slices.Sorted(maps.Keys(fields)) {
		b = appendBool(b, num, *fields[num])
	}

	return appendInt32(b, fieldConfigNSLevel, cfg.NSLevel)
}

func float32sToBytes(x []float32) []byte {
	out := make([]byte, len(x)*4)
	for i, v := range x {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}

	return out
}
