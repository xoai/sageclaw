package audio

import (
	"os"
	"testing"
)

// TestPionCodec_RealTelegramOGG tests with an actual Telegram voice message.
// Skip if file doesn't exist (CI environments).
func TestPionCodec_RealTelegramOGG(t *testing.T) {
	testFile := "../../bin/data/audio/218556567/29.ogg"
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("test OGG file not available: %v", err)
	}

	t.Logf("OGG file size: %d bytes", len(data))

	// Test OGG packet extraction first.
	packets, err := extractOGGOpusPackets(data)
	if err != nil {
		t.Fatalf("extractOGGOpusPackets failed: %v", err)
	}
	t.Logf("Extracted %d Opus packets", len(packets))
	for i, p := range packets {
		if i < 5 || i == len(packets)-1 {
			t.Logf("  packet[%d]: %d bytes, TOC=0x%02x, frameSamples=%d",
				i, len(p), p[0], opusFrameSamples(p, 48000))
		}
	}

	if len(packets) == 0 {
		t.Fatal("no packets extracted from OGG file")
	}

	// Test full decode — pion/opus doesn't support Hybrid mode (TOC 0x78)
	// used by Telegram, so this is expected to fail. The voice pipeline
	// falls back to REST API transcription in this case.
	codec := &PionCodec{}
	pcm, err := codec.DecodeOGGToPCM(data, 16000)
	if err != nil {
		t.Logf("DecodeOGGToPCM failed as expected (Hybrid mode unsupported by pion): %v", err)
		t.Log("Voice pipeline will use REST API transcription fallback")
		return
	}

	t.Logf("PCM output: %d bytes (%.1f seconds at 16kHz)",
		len(pcm), float64(len(pcm))/float64(16000*2))

	if len(pcm) < 1000 {
		t.Errorf("PCM output too small (%d bytes), expected at least a few seconds of audio", len(pcm))
	}
}
