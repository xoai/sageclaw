package audio

import (
	"bytes"
	"fmt"
	"log"

	godepsopus "github.com/godeps/opus"
)

// GodepsCodec is a pure-Go OGG Opus codec using github.com/godeps/opus.
// Uses a WASM-compiled libopus via wazero — supports ALL Opus modes
// including Hybrid (SILK+CELT) used by Telegram voice messages.
// No CGo, no ffmpeg, no external dependencies. Always available.
type GodepsCodec struct{}

// Available always returns true — pure Go via WASM, no external deps.
func (c *GodepsCodec) Available() bool { return true }

// Name returns the codec name.
func (c *GodepsCodec) Name() string { return "godeps-opus" }

// DecodeOGGToPCM decodes OGG Opus audio to raw PCM (16-bit LE, mono).
func (c *GodepsCodec) DecodeOGGToPCM(oggData []byte, targetSampleRate int) ([]byte, error) {
	if len(oggData) == 0 {
		return nil, ErrEmptyInput
	}

	// Extract Opus packets from OGG container using our parser.
	packets, err := extractOGGOpusPackets(oggData)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecodeFailed, err)
	}
	if len(packets) == 0 {
		return nil, fmt.Errorf("%w: no Opus packets found in OGG", ErrDecodeFailed)
	}

	// Create decoder at 48kHz mono (Opus native rate).
	decoder, err := godepsopus.NewDecoder(48000, 1)
	if err != nil {
		return nil, fmt.Errorf("%w: create decoder: %v", ErrDecodeFailed, err)
	}

	var pcmBuf bytes.Buffer
	pcmBuf.Grow(len(packets) * 960 * PCMBytesPerSample)

	// Max Opus frame = 5760 samples at 48kHz (120ms).
	outSamples := make([]int16, 5760)
	decodedFrames := 0

	for _, packet := range packets {
		n, err := decoder.Decode(packet, outSamples)
		if err != nil || n <= 0 {
			continue
		}

		// Convert int16 samples to S16LE bytes.
		for i := 0; i < n; i++ {
			pcmBuf.WriteByte(byte(outSamples[i]))
			pcmBuf.WriteByte(byte(outSamples[i] >> 8))
		}
		decodedFrames++
	}

	log.Printf("godeps-opus: %d packets, %d decoded, %d bytes PCM (48kHz)",
		len(packets), decodedFrames, pcmBuf.Len())

	if pcmBuf.Len() == 0 {
		return nil, fmt.Errorf("%w: no audio decoded from %d packets", ErrDecodeFailed, len(packets))
	}

	pcmData := pcmBuf.Bytes()

	// Resample from 48kHz to target rate if needed.
	const opusNativeSampleRate = 48000
	if targetSampleRate != opusNativeSampleRate && targetSampleRate > 0 {
		resampled, err := ResamplePCM(pcmData, opusNativeSampleRate, targetSampleRate)
		if err != nil {
			return nil, fmt.Errorf("%w: resample %d→%d: %v", ErrDecodeFailed, opusNativeSampleRate, targetSampleRate, err)
		}
		return resampled, nil
	}

	return pcmData, nil
}

// EncodePCMToOGG converts raw PCM (16-bit LE, mono) to OGG Opus.
func (c *GodepsCodec) EncodePCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	if len(pcmData) == 0 {
		return nil, ErrEmptyInput
	}

	// Convert bytes to int16 samples.
	numSamples := len(pcmData) / PCMBytesPerSample
	samples := make([]int16, numSamples)
	for i := 0; i < numSamples; i++ {
		samples[i] = int16(pcmData[2*i]) | int16(pcmData[2*i+1])<<8
	}

	// Create Opus encoder.
	encoder, err := godepsopus.NewEncoder(sampleRate, 1, godepsopus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("%w: create encoder: %v", ErrEncodeFailed, err)
	}

	// Encode in 20ms frames.
	frameSamples := sampleRate * 20 / 1000 // e.g. 480 at 24kHz
	maxPacketSize := 4000
	opusBuf := make([]byte, maxPacketSize)

	var opusFrames [][]byte
	for offset := 0; offset+frameSamples <= numSamples; offset += frameSamples {
		frame := samples[offset : offset+frameSamples]
		n, err := encoder.Encode(frame, opusBuf)
		if err != nil || n <= 0 {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, opusBuf[:n])
		opusFrames = append(opusFrames, pkt)
	}

	if len(opusFrames) == 0 {
		return nil, fmt.Errorf("%w: no frames encoded", ErrEncodeFailed)
	}

	// Wrap Opus frames in OGG container.
	ogg, err := buildOGGOpus(opusFrames, sampleRate, frameSamples)
	if err != nil {
		return nil, fmt.Errorf("%w: build OGG: %v", ErrEncodeFailed, err)
	}

	log.Printf("godeps-opus: encoded %d PCM bytes → %d OGG bytes (%d frames)",
		len(pcmData), len(ogg), len(opusFrames))

	return ogg, nil
}

// Ensure GodepsCodec satisfies the Codec interface.
var _ Codec = (*GodepsCodec)(nil)
