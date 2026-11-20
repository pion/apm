// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package wav provides streaming WAV I/O for PCM int16 and IEEE float32
// formats at any sample rate. It is deliberately minimal: only the subset
// needed for APM testing is implemented.
//
// Read example:
//
//	r, err := wav.OpenReader("clip.wav")
//	if err != nil { ... }
//	defer r.Close()
//	buf := make([]float32, r.Channels()*1024)
//	for {
//	    n, err := r.ReadFloat32(buf)
//	    if n > 0 { /* process buf[:n] */ }
//	    if err == io.EOF { break }
//	    if err != nil { ... }
//	}
//
// Write example:
//
//	w, err := wav.CreateWriter("out.wav", 16000, 1, wav.PCM16)
//	if err != nil { ... }
//	defer w.Close()
//	w.WriteFloat32(samples)
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

var (
	// ErrInvalidHeader is returned for malformed RIFF/WAVE headers.
	ErrInvalidHeader = errors.New("wav: invalid header")
	// ErrUnsupportedFormat is returned for sample formats other than PCM16 and float32.
	ErrUnsupportedFormat = errors.New("wav: unsupported format")
	// ErrInvalidChannels is returned for empty or ragged channel layouts.
	ErrInvalidChannels = errors.New("wav: invalid channels")
	// ErrInvalidSampleRate is returned for sample rates that do not fit the header.
	ErrInvalidSampleRate = errors.New("wav: invalid sample rate")
	// ErrTooLarge is returned when the data no longer fits a 32-bit RIFF size.
	ErrTooLarge = errors.New("wav: data exceeds 4 GiB")
)

// Encoding selects the WAV sample encoding written by Writer.
type Encoding int

const (
	// PCM16 is signed 16-bit linear PCM (format code 1).
	PCM16 Encoding = iota
	// Float32 is 32-bit IEEE float (format code 3).
	Float32
)

const (
	formatPCM   uint16 = 1
	formatFloat uint16 = 3
	formatExt   uint16 = 0xFFFE
)

// Reader streams samples out of a WAV file.
type Reader struct {
	rc          io.ReadCloser
	channels    int
	sampleRate  int
	bitsPerSamp int
	format      uint16
	dataLeft    int64 // bytes remaining in the data chunk
	scratch     []byte
}

// OpenReader opens a WAV file for streaming reads.
func OpenReader(path string) (*Reader, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	r, err := NewReader(f)
	if err != nil {
		_ = f.Close()

		return nil, err
	}

	return r, nil
}

// NewReader wraps an arbitrary ReadCloser as a WAV reader. The header is
// parsed eagerly; subsequent reads consume the data chunk.
func NewReader(rc io.ReadCloser) (*Reader, error) {
	r := &Reader{rc: rc}
	if err := r.readHeader(); err != nil {
		return nil, err
	}

	return r, nil
}

// Channels returns the channel count.
func (r *Reader) Channels() int { return r.channels }

// SampleRate returns the sample rate in Hz.
func (r *Reader) SampleRate() int { return r.sampleRate }

// BitsPerSample returns the encoded sample width in bits.
func (r *Reader) BitsPerSample() int { return r.bitsPerSamp }

// Close releases the underlying file.
func (r *Reader) Close() error { return r.rc.Close() }

// ReadFloat32 reads interleaved samples into buf, converting to float32 in
// [-1, 1]. Returns the number of samples written (length, in samples, not
// frames) and io.EOF when the data chunk is exhausted.
func (r *Reader) ReadFloat32(buf []float32) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	bytesPerSample := r.bitsPerSamp / 8
	maxSamples := int(r.dataLeft) / bytesPerSample
	if maxSamples == 0 {
		return 0, io.EOF
	}
	maxSamples = min(maxSamples, len(buf))
	need := maxSamples * bytesPerSample
	if cap(r.scratch) < need {
		r.scratch = make([]byte, need)
	}
	r.scratch = r.scratch[:need]
	n, err := io.ReadFull(r.rc, r.scratch)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, err
	}
	samples := n / bytesPerSample
	r.dataLeft -= int64(n)
	if err := r.decode(buf[:samples]); err != nil {
		return 0, err
	}

	return samples, nil
}

// decode converts len(buf) samples from r.scratch into buf.
func (r *Reader) decode(buf []float32) error {
	switch {
	case r.format == formatPCM && r.bitsPerSamp == 16:
		for i := range buf {
			v := int16(binary.LittleEndian.Uint16(r.scratch[i*2:])) //nolint:gosec // Two's complement reinterpretation.
			buf[i] = float32(v) / 32768
		}
	case r.format == formatFloat && r.bitsPerSamp == 32:
		for i := range buf {
			b := binary.LittleEndian.Uint32(r.scratch[i*4:])
			buf[i] = math.Float32frombits(b)
		}
	default:
		return fmt.Errorf("%w: format=%d bits=%d", ErrUnsupportedFormat, r.format, r.bitsPerSamp)
	}

	return nil
}

func (r *Reader) readHeader() error {
	var hdr [12]byte
	if _, err := io.ReadFull(r.rc, hdr[:]); err != nil {
		return fmt.Errorf("wav: short header: %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return fmt.Errorf("%w: not a RIFF/WAVE file", ErrInvalidHeader)
	}
	for {
		var chunkHdr [8]byte
		if _, err := io.ReadFull(r.rc, chunkHdr[:]); err != nil {
			return fmt.Errorf("wav: missing data chunk: %w", err)
		}
		done, err := r.readChunk(string(chunkHdr[0:4]), binary.LittleEndian.Uint32(chunkHdr[4:8]))
		if err != nil || done {
			return err
		}
	}
}

// readChunk consumes one chunk body. done is true once the data chunk is reached.
func (r *Reader) readChunk(id string, size uint32) (done bool, err error) {
	switch id {
	case "fmt ":
		return false, r.readFmt(int(size))
	case "data":
		r.dataLeft = int64(size)

		return true, nil
	default:
		if _, err := io.CopyN(io.Discard, r.rc, int64(size)); err != nil {
			return false, fmt.Errorf("wav: skip %q chunk: %w", id, err)
		}

		return false, nil
	}
}

func (r *Reader) readFmt(size int) error {
	if size < 16 {
		return fmt.Errorf("%w: fmt chunk too small (%d)", ErrInvalidHeader, size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r.rc, buf); err != nil {
		return err
	}
	r.format = binary.LittleEndian.Uint16(buf[0:2])
	r.channels = int(binary.LittleEndian.Uint16(buf[2:4]))
	r.sampleRate = int(binary.LittleEndian.Uint32(buf[4:8]))
	r.bitsPerSamp = int(binary.LittleEndian.Uint16(buf[14:16]))
	if r.format == formatExt && size >= 40 {
		// SubFormat GUID first 2 bytes are the actual format code.
		r.format = binary.LittleEndian.Uint16(buf[24:26])
	}
	if r.channels <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidChannels, r.channels)
	}
	if r.sampleRate <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidSampleRate, r.sampleRate)
	}
	if r.format != formatPCM && r.format != formatFloat {
		return fmt.Errorf("%w: format code %d", ErrUnsupportedFormat, r.format)
	}

	return nil
}

// Writer streams samples into a WAV file.
//
// The RIFF and data chunk sizes are not known until Close, so Writer
// requires a seeker (os.File satisfies this). Writes are buffered to disk
// in the order received.
type Writer struct {
	f          *os.File
	channels   int
	sampleRate int
	encoding   Encoding
	dataStart  int64
	dataBytes  int64
	scratch    []byte
}

// CreateWriter creates a new WAV file and writes its header. Caller must
// Close the writer to finalize the file.
func CreateWriter(path string, sampleRate, channels int, enc Encoding) (*Writer, error) {
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	w := &Writer{f: f, channels: channels, sampleRate: sampleRate, encoding: enc}
	if err := w.writeHeader(); err != nil {
		_ = f.Close()

		return nil, err
	}

	return w, nil
}

func (w *Writer) writeHeader() error {
	if w.channels <= 0 || w.channels > math.MaxUint16 {
		return fmt.Errorf("%w: %d", ErrInvalidChannels, w.channels)
	}
	if w.sampleRate <= 0 || w.sampleRate > math.MaxInt32 {
		return fmt.Errorf("%w: %d", ErrInvalidSampleRate, w.sampleRate)
	}
	channels := uint16(w.channels)
	sampleRate := uint32(w.sampleRate)
	bitsPerSamp := uint16(16)
	formatCode := formatPCM
	if w.encoding == Float32 {
		bitsPerSamp = 32
		formatCode = formatFloat
	}
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSamp/8)
	blockAlign := channels * (bitsPerSamp / 8)

	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 0) // patched on Close
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], formatCode)
	binary.LittleEndian.PutUint16(hdr[22:24], channels)
	binary.LittleEndian.PutUint32(hdr[24:28], sampleRate)
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], bitsPerSamp)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], 0) // patched on Close

	if _, err := w.f.Write(hdr[:]); err != nil {
		return err
	}
	w.dataStart = 44

	return nil
}

// WriteFloat32 appends interleaved float32 samples (range [-1, 1]).
// Values outside that range are clipped on PCM16 encoding.
func (w *Writer) WriteFloat32(samples []float32) error {
	if len(samples) == 0 {
		return nil
	}
	switch w.encoding {
	case PCM16:
		w.encodePCM16(samples)
	case Float32:
		w.encodeFloat32(samples)
	default:
		return nil
	}
	if _, err := w.f.Write(w.scratch); err != nil {
		return err
	}
	w.dataBytes += int64(len(w.scratch))

	return nil
}

func (w *Writer) grow(need int) {
	if cap(w.scratch) < need {
		w.scratch = make([]byte, need)
	}
	w.scratch = w.scratch[:need]
}

func (w *Writer) encodePCM16(samples []float32) {
	w.grow(len(samples) * 2)
	for i, s := range samples {
		v := s * 32767
		switch {
		case v > 32767:
			v = 32767
		case v < -32768:
			v = -32768
		}
		binary.LittleEndian.PutUint16(w.scratch[i*2:], uint16(int16(v))) //nolint:gosec // Two's complement reinterpretation.
	}
}

func (w *Writer) encodeFloat32(samples []float32) {
	w.grow(len(samples) * 4)
	for i, s := range samples {
		binary.LittleEndian.PutUint32(w.scratch[i*4:], math.Float32bits(s))
	}
}

// Close patches the RIFF and data chunk sizes, then closes the file.
func (w *Writer) Close() error {
	if w.f == nil {
		return nil
	}
	defer func() { w.f = nil }()
	dataBytes := w.dataBytes
	if dataBytes < 0 || dataBytes > math.MaxUint32-36 {
		_ = w.f.Close()

		return ErrTooLarge
	}
	if _, err := w.f.Seek(4, io.SeekStart); err != nil {
		return err
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(36+dataBytes))
	if _, err := w.f.Write(buf[:]); err != nil {
		return err
	}
	if _, err := w.f.Seek(40, io.SeekStart); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(buf[:], uint32(dataBytes))
	if _, err := w.f.Write(buf[:]); err != nil {
		return err
	}

	return w.f.Close()
}

// ReadAll loads an entire WAV file as deinterleaved float32 channels.
// Convenience for small test fixtures only: do not use for long clips.
func ReadAll(path string) (channels [][]float32, sampleRate int, err error) {
	reader, err := OpenReader(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = reader.Close() }()
	chans := reader.Channels()
	channels = make([][]float32, chans)
	buf := make([]float32, chans*4096)
	for {
		n, err := reader.ReadFloat32(buf)
		for i := range n {
			channels[i%chans] = append(channels[i%chans], buf[i])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, err
		}
	}

	return channels, reader.SampleRate(), nil
}

// WriteAll writes deinterleaved float32 channels to a WAV file at the
// given rate and encoding. Convenience for tests; all channels must have
// equal length.
func WriteAll(path string, channels [][]float32, sampleRate int, enc Encoding) error {
	if len(channels) == 0 {
		return fmt.Errorf("%w: WriteAll requires at least one channel", ErrInvalidChannels)
	}
	n := len(channels[0])
	for i, ch := range channels {
		if len(ch) != n {
			return fmt.Errorf("%w: channel %d has %d samples, want %d", ErrInvalidChannels, i, len(ch), n)
		}
	}
	writer, err := CreateWriter(path, sampleRate, len(channels), enc)
	if err != nil {
		return err
	}
	buf := make([]float32, len(channels)*1024)
	for i := 0; i < n; i += 1024 {
		end := min(i+1024, n)
		k := 0
		for s := i; s < end; s++ {
			for c := range channels {
				buf[k] = channels[c][s]
				k++
			}
		}
		if err := writer.WriteFloat32(buf[:k]); err != nil {
			_ = writer.Close()

			return err
		}
	}

	return writer.Close()
}
