package audio

import (
	"os"
	"testing"
)

func TestGodepsCodec_RealTelegramOGG(t *testing.T) {
	testFile := "../../bin/data/audio/218556567/29.ogg"
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("test OGG file not available: %v", err)
	}

	t.Logf("OGG file size: %d bytes", len(data))

	codec := &GodepsCodec{}
	pcm, err := codec.DecodeOGGToPCM(data, 16000)
	if err != nil {
		t.Fatalf("DecodeOGGToPCM failed: %v", err)
	}

	durationMs := PCMDurationMs(pcm, 16000)
	t.Logf("PCM output: %d bytes (%.1f seconds at 16kHz)", len(pcm), float64(durationMs)/1000)

	if durationMs < 500 {
		t.Errorf("decoded audio too short (%dms), expected at least 500ms", durationMs)
	}
}
