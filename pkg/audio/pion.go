package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/pion/opus"
)

// PionCodec is a pure-Go OGG Opus decoder using github.com/pion/opus.
// No external dependencies (no ffmpeg, no CGo). Always available.
// Supports decoding OGG→PCM. Encoding PCM→OGG is NOT supported
// (response audio falls back to transcript text).
type PionCodec struct{}

// Available always returns true — pure Go, no external deps.
func (c *PionCodec) Available() bool { return true }

// Name returns the codec name.
func (c *PionCodec) Name() string { return "pion-opus" }

// DecodeOGGToPCM decodes OGG Opus audio to raw PCM (16-bit LE, mono).
func (c *PionCodec) DecodeOGGToPCM(oggData []byte, targetSampleRate int) ([]byte, error) {
	if len(oggData) == 0 {
		return nil, ErrEmptyInput
	}

	// Extract Opus packets from OGG container.
	packets, err := extractOGGOpusPackets(oggData)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecodeFailed, err)
	}

	if len(packets) == 0 {
		return nil, fmt.Errorf("%w: no Opus packets found in OGG", ErrDecodeFailed)
	}

	decoder := opus.NewDecoder()

	// Pre-allocate output buffer.
	var pcmBuf bytes.Buffer
	pcmBuf.Grow(len(packets) * 960 * PCMBytesPerSample) // ~20ms per packet at 48kHz

	// Max Opus frame at 48kHz = 5760 samples (120ms) * 2 bytes = 11520 bytes.
	outFrame := make([]byte, 5760*PCMBytesPerSample)
	decodedFrames := 0

	for _, packet := range packets {
		// Decode the Opus packet to S16LE PCM.
		_, _, decErr := decoder.Decode(packet, outFrame)
		if decErr != nil {
			continue
		}

		// Calculate actual frame size from the Opus TOC byte.
		frameSamples := opusFrameSamples(packet, 48000)
		frameBytes := frameSamples * PCMBytesPerSample
		if frameBytes > len(outFrame) {
			frameBytes = len(outFrame)
		}
		if frameBytes > 0 {
			pcmBuf.Write(outFrame[:frameBytes])
			decodedFrames++
		}
	}

	log.Printf("pion-opus: %d Opus packets extracted, %d decoded, %d bytes PCM",
		len(packets), decodedFrames, pcmBuf.Len())

	if pcmBuf.Len() == 0 {
		return nil, fmt.Errorf("%w: no audio frames decoded from %d packets", ErrDecodeFailed, len(packets))
	}

	pcmData := pcmBuf.Bytes()

	// Pion decodes at 48kHz. Resample to target rate if needed.
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

// EncodePCMToOGG is not supported by the pure-Go codec.
func (c *PionCodec) EncodePCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	return nil, fmt.Errorf("%w: PionCodec does not support encoding (install ffmpeg for voice responses)", ErrEncodeFailed)
}

// extractOGGOpusPackets parses an OGG container and extracts Opus audio packets.
// Handles OGG page structure: magic bytes, segment table, segment reassembly.
// Skips the first 2 logical pages (OpusHead header + OpusTags comment).
// Reference: RFC 3533 (OGG), RFC 7845 (Opus in OGG).
func extractOGGOpusPackets(data []byte) ([][]byte, error) {
	r := bytes.NewReader(data)
	var packets [][]byte
	pageNum := 0

	for {
		// Read OGG page header: "OggS" capture pattern.
		var magic [4]byte
		if _, err := io.ReadFull(r, magic[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, fmt.Errorf("read OGG magic: %w", err)
		}
		if string(magic[:]) != "OggS" {
			return nil, fmt.Errorf("invalid OGG page (expected OggS, got %q)", magic)
		}

		// Read rest of the 27-byte page header.
		var hdr [23]byte // 27 total - 4 magic = 23 remaining
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, fmt.Errorf("read OGG header: %w", err)
		}

		// Byte 4: stream structure version (must be 0).
		// Byte 5: header type flag.
		// Bytes 6-13: granule position.
		// Bytes 14-17: serial number.
		// Bytes 18-21: page sequence number.
		// Bytes 22-25: CRC checksum.
		// Byte 26: number of segments.
		numSegments := int(hdr[22]) // Offset 26 - 4 = 22 in our hdr slice.

		// Read segment table.
		segTable := make([]byte, numSegments)
		if _, err := io.ReadFull(r, segTable); err != nil {
			return nil, fmt.Errorf("read segment table: %w", err)
		}

		// Read all segment data for this page.
		var totalSize int
		for _, s := range segTable {
			totalSize += int(s)
		}
		pageData := make([]byte, totalSize)
		if _, err := io.ReadFull(r, pageData); err != nil {
			return nil, fmt.Errorf("read page data: %w", err)
		}

		pageNum++

		// Skip first 2 pages (OpusHead + OpusTags).
		if pageNum <= 2 {
			continue
		}

		// Reassemble segments into Opus packets.
		// In OGG, a packet ends when a segment is < 255 bytes.
		// A segment of exactly 255 bytes means the packet continues
		// in the next segment.
		offset := 0
		var currentPacket []byte
		for _, segSize := range segTable {
			seg := pageData[offset : offset+int(segSize)]
			offset += int(segSize)
			currentPacket = append(currentPacket, seg...)

			if segSize < 255 {
				// Packet boundary — this packet is complete.
				if len(currentPacket) > 0 {
					pkt := make([]byte, len(currentPacket))
					copy(pkt, currentPacket)
					packets = append(packets, pkt)
				}
				currentPacket = currentPacket[:0]
			}
			// If segSize == 255, packet continues in next segment.
		}

		// If the last segment was 255, the packet spans to the next page.
		// We'd need cross-page reassembly for that. For typical Telegram
		// voice notes (20ms Opus frames ≈ 40-80 bytes), this won't happen.
		if len(currentPacket) > 0 {
			pkt := make([]byte, len(currentPacket))
			copy(pkt, currentPacket)
			packets = append(packets, pkt)
		}
	}

	return packets, nil
}

// opusFrameSamples returns the number of mono PCM samples in an Opus packet
// based on the TOC byte (RFC 6716, Section 3.1).
func opusFrameSamples(packet []byte, sampleRate int) int {
	if len(packet) == 0 {
		return 0
	}

	toc := packet[0]
	config := toc >> 3 // Top 5 bits = configuration number (0-31).

	// Frame duration in microseconds based on config number.
	var frameDurationUs int
	switch {
	case config <= 3:
		frameDurationUs = []int{10000, 20000, 40000, 60000}[config]
	case config <= 7:
		frameDurationUs = []int{10000, 20000, 40000, 60000}[config-4]
	case config <= 11:
		frameDurationUs = []int{10000, 20000, 40000, 60000}[config-8]
	case config <= 13:
		frameDurationUs = []int{10000, 20000}[config-12]
	case config <= 15:
		frameDurationUs = []int{10000, 20000}[config-14]
	case config <= 19:
		frameDurationUs = []int{2500, 5000, 10000, 20000}[config-16]
	case config <= 23:
		frameDurationUs = []int{2500, 5000, 10000, 20000}[config-20]
	case config <= 27:
		frameDurationUs = []int{2500, 5000, 10000, 20000}[config-24]
	case config <= 31:
		frameDurationUs = []int{2500, 5000, 10000, 20000}[config-28]
	default:
		frameDurationUs = 20000
	}

	// Number of frames from TOC code bits.
	code := toc & 0x03
	numFrames := 1
	switch code {
	case 0:
		numFrames = 1
	case 1, 2:
		numFrames = 2
	case 3:
		if len(packet) > 1 {
			numFrames = int(packet[1] & 0x3F)
		}
	}

	samplesPerFrame := (sampleRate * frameDurationUs) / 1_000_000
	return samplesPerFrame * numFrames
}

// Ensure PionCodec satisfies the Codec interface.
var _ Codec = (*PionCodec)(nil)

// unused import guard
var _ = binary.LittleEndian
