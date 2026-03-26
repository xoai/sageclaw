// Package audio provides audio codec operations for voice messaging.
//
// Converts between OGG Opus (Telegram voice format) and raw PCM
// (Gemini Live native audio format). Uses an exec-based approach
// calling ffmpeg/opusdec/opusenc when available, with a pure-Go
// fallback for OGG container parsing.
package audio

import (
	"errors"
	"fmt"
)

// PCM format constants for Gemini Live API.
const (
	GeminiInputSampleRate  = 16000 // 16 kHz mono, 16-bit LE PCM
	GeminiOutputSampleRate = 24000 // 24 kHz mono, 16-bit LE PCM
	PCMBytesPerSample      = 2     // 16-bit = 2 bytes
	PCMChannels            = 1     // Mono
)

var (
	ErrNoCodec       = errors.New("audio: no codec available (install ffmpeg or opus-tools)")
	ErrDecodeFailed  = errors.New("audio: OGG to PCM decode failed")
	ErrEncodeFailed  = errors.New("audio: PCM to OGG encode failed")
	ErrResampleFailed = errors.New("audio: PCM resample failed")
	ErrEmptyInput    = errors.New("audio: empty input data")
)

// Codec defines the interface for audio format conversion.
type Codec interface {
	// DecodeOGGToPCM converts OGG Opus audio to raw PCM (16-bit LE, mono).
	// Returns PCM data and the sample rate of the output.
	DecodeOGGToPCM(oggData []byte, targetSampleRate int) (pcm []byte, err error)

	// EncodePCMToOGG converts raw PCM (16-bit LE, mono) to OGG Opus.
	EncodePCMToOGG(pcmData []byte, sampleRate int) (ogg []byte, err error)

	// Available returns true if this codec backend is usable.
	Available() bool

	// Name returns the codec backend name (e.g. "ffmpeg", "opus-tools").
	Name() string
}

// DefaultCodec returns the best available codec.
// Prefers ffmpeg (full encode+decode) → opus-tools → pion (pure-Go decode only).
// Always returns a codec — PionCodec is the pure-Go fallback that requires no
// external dependencies.
func DefaultCodec() (Codec, error) {
	// Try ffmpeg first (most capable — both encode and decode).
	c := &FFmpegCodec{}
	if c.Available() {
		return c, nil
	}

	// Try opus-tools (opusdec/opusenc).
	o := &OpusToolsCodec{}
	if o.Available() {
		return o, nil
	}

	// Pure-Go fallback via WASM-compiled libopus — always available.
	// Supports all Opus modes including Hybrid (used by Telegram).
	// Decode-only; voice responses fall back to transcript text.
	return &GodepsCodec{}, nil
}

// PCMDurationMs calculates the duration of PCM data in milliseconds.
func PCMDurationMs(pcmData []byte, sampleRate int) int {
	if sampleRate == 0 || len(pcmData) == 0 {
		return 0
	}
	samples := len(pcmData) / PCMBytesPerSample / PCMChannels
	return (samples * 1000) / sampleRate
}

// ResamplePCM resamples PCM data from one sample rate to another using
// linear interpolation. Good enough for voice; not audiophile quality.
func ResamplePCM(data []byte, fromRate, toRate int) ([]byte, error) {
	if fromRate == toRate {
		return data, nil
	}
	if len(data) == 0 {
		return nil, ErrEmptyInput
	}
	if fromRate <= 0 || toRate <= 0 {
		return nil, fmt.Errorf("%w: invalid sample rates %d → %d", ErrResampleFailed, fromRate, toRate)
	}

	// Convert bytes to int16 samples.
	numSamples := len(data) / PCMBytesPerSample
	if numSamples == 0 {
		return nil, ErrEmptyInput
	}

	inSamples := make([]int16, numSamples)
	for i := 0; i < numSamples; i++ {
		inSamples[i] = int16(data[2*i]) | int16(data[2*i+1])<<8
	}

	// Calculate output length.
	outLen := int(float64(numSamples) * float64(toRate) / float64(fromRate))
	outSamples := make([]int16, outLen)

	// Linear interpolation.
	ratio := float64(fromRate) / float64(toRate)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx+1 < numSamples {
			outSamples[i] = int16(float64(inSamples[srcIdx])*(1-frac) + float64(inSamples[srcIdx+1])*frac)
		} else if srcIdx < numSamples {
			outSamples[i] = inSamples[srcIdx]
		}
	}

	// Convert back to bytes.
	out := make([]byte, outLen*PCMBytesPerSample)
	for i, s := range outSamples {
		out[2*i] = byte(s)
		out[2*i+1] = byte(s >> 8)
	}

	return out, nil
}
