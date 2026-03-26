package audio

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

// buildOGGOpus wraps Opus frames in a minimal OGG container.
// Produces a valid OGG Opus file with OpusHead + OpusTags headers.
// Reference: RFC 7845 (Ogg Encapsulation for Opus).
func buildOGGOpus(frames [][]byte, sampleRate, frameSamples int) ([]byte, error) {
	var buf bytes.Buffer
	serialNo := uint32(0x53616765) // "Sage"

	// Page 0: OpusHead header.
	opusHead := buildOpusHead(sampleRate)
	writeOGGPage(&buf, serialNo, 0, 0, 2, [][]byte{opusHead}) // flags=2 (BOS)

	// Page 1: OpusTags (comment) header.
	opusTags := buildOpusTags()
	writeOGGPage(&buf, serialNo, 1, 0, 0, [][]byte{opusTags})

	// Audio pages: one Opus frame per segment, multiple frames per page.
	// Group up to 48 frames per page (about 1 second at 20ms frames).
	const framesPerPage = 48
	granulePos := uint64(0)
	pageSeqNo := uint32(2)

	for i := 0; i < len(frames); i += framesPerPage {
		end := i + framesPerPage
		if end > len(frames) {
			end = len(frames)
		}
		pageFrames := frames[i:end]

		// Calculate granule position (cumulative samples at 48kHz).
		// Opus always uses 48kHz for granule position regardless of input rate.
		samplesAt48k := frameSamples * 48000 / sampleRate
		granulePos += uint64(len(pageFrames) * samplesAt48k)

		flags := byte(0)
		if end >= len(frames) {
			flags = 4 // EOS (end of stream) on last page.
		}

		writeOGGPage(&buf, serialNo, pageSeqNo, granulePos, flags, pageFrames)
		pageSeqNo++
	}

	return buf.Bytes(), nil
}

// buildOpusHead creates the OpusHead identification header (RFC 7845 5.1).
func buildOpusHead(sampleRate int) []byte {
	var buf bytes.Buffer
	buf.WriteString("OpusHead")         // Magic signature
	buf.WriteByte(1)                    // Version
	buf.WriteByte(1)                    // Channel count (mono)
	binary.Write(&buf, binary.LittleEndian, uint16(0))          // Pre-skip (samples)
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate)) // Input sample rate
	binary.Write(&buf, binary.LittleEndian, int16(0))           // Output gain
	buf.WriteByte(0)                    // Channel mapping family (0 = mono/stereo)
	return buf.Bytes()
}

// buildOpusTags creates the OpusTags comment header (RFC 7845 5.2).
func buildOpusTags() []byte {
	var buf bytes.Buffer
	buf.WriteString("OpusTags")
	vendor := "SageClaw"
	binary.Write(&buf, binary.LittleEndian, uint32(len(vendor)))
	buf.WriteString(vendor)
	binary.Write(&buf, binary.LittleEndian, uint32(0)) // No user comments
	return buf.Bytes()
}

// writeOGGPage writes a single OGG page to the buffer.
// Reference: RFC 3533 section 6.
func writeOGGPage(buf *bytes.Buffer, serialNo, seqNo uint32, granulePos uint64, flags byte, segments [][]byte) {
	// Build segment table.
	var segTable []byte
	for _, seg := range segments {
		remaining := len(seg)
		for remaining >= 255 {
			segTable = append(segTable, 255)
			remaining -= 255
		}
		segTable = append(segTable, byte(remaining))
	}

	// Build page header (27 bytes + segment table).
	var hdr bytes.Buffer
	hdr.WriteString("OggS")                                              // Capture pattern
	hdr.WriteByte(0)                                                      // Version
	hdr.WriteByte(flags)                                                  // Header type
	binary.Write(&hdr, binary.LittleEndian, granulePos)                   // Granule position
	binary.Write(&hdr, binary.LittleEndian, serialNo)                     // Serial number
	binary.Write(&hdr, binary.LittleEndian, seqNo)                        // Page sequence number
	binary.Write(&hdr, binary.LittleEndian, uint32(0))                    // CRC placeholder
	hdr.WriteByte(byte(len(segTable)))                                    // Number of segments
	hdr.Write(segTable)                                                   // Segment table

	// Concatenate segment data.
	var segData bytes.Buffer
	for _, seg := range segments {
		segData.Write(seg)
	}

	// Calculate CRC32 over entire page (header + segment data).
	pageBytes := append(hdr.Bytes(), segData.Bytes()...)
	crc := crc32OGG(pageBytes)
	// Patch CRC into header at offset 22.
	pageBytes[22] = byte(crc)
	pageBytes[23] = byte(crc >> 8)
	pageBytes[24] = byte(crc >> 16)
	pageBytes[25] = byte(crc >> 24)

	buf.Write(pageBytes)
}

// crc32OGG computes the OGG CRC-32 (polynomial 0x04C11DB7).
// This is NOT the standard CRC32 — OGG uses a different polynomial.
func crc32OGG(data []byte) uint32 {
	// OGG uses CRC-32 with polynomial 0x04C11DB7 (normal form, not reflected).
	// This is the same as MPEG-2 CRC.
	if oggCRCTable == nil {
		oggCRCTable = buildOGGCRCTable()
	}

	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[(crc>>24)^uint32(b)]
	}
	return crc
}

var oggCRCTable []uint32

func buildOGGCRCTable() []uint32 {
	table := make([]uint32, 256)
	const poly = 0x04C11DB7
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}

// Ensure crc32 import is used (for documentation reference).
var _ = crc32.ChecksumIEEE
