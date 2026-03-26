package audio

import (
	"os"
	"testing"

	"github.com/pion/opus"
)

func TestPionDecoder_DirectDecode(t *testing.T) {
	testFile := "../../bin/data/audio/218556567/29.ogg"
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}

	packets, err := extractOGGOpusPackets(data)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	decoder := opus.NewDecoder()

	// Try decoding each packet and log the error.
	outBuf := make([]byte, 5760*2)
	for i, pkt := range packets {
		if i > 5 {
			break
		}
		bw, stereo, err := decoder.Decode(pkt, outBuf)
		t.Logf("packet[%d] len=%d TOC=0x%02x → bw=%v stereo=%v err=%v",
			i, len(pkt), pkt[0], bw, stereo, err)

		// Also try DecodeFloat32.
		outF32 := make([]float32, 5760)
		bw2, stereo2, err2 := decoder.DecodeFloat32(pkt, outF32)
		t.Logf("  float32: bw=%v stereo=%v err=%v", bw2, stereo2, err2)
	}
}
