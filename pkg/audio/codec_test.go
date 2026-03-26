package audio

import (
	"testing"
)

func TestPCMDurationMs(t *testing.T) {
	tests := []struct {
		name       string
		dataLen    int
		sampleRate int
		wantMs     int
	}{
		{"1 second at 16kHz", 32000, 16000, 1000},
		{"500ms at 24kHz", 24000, 24000, 500},
		{"empty", 0, 16000, 0},
		{"zero rate", 100, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.dataLen)
			got := PCMDurationMs(data, tt.sampleRate)
			if got != tt.wantMs {
				t.Errorf("PCMDurationMs(%d bytes, %d Hz) = %d, want %d", tt.dataLen, tt.sampleRate, got, tt.wantMs)
			}
		})
	}
}

func TestResamplePCM_SameRate(t *testing.T) {
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}

	out, err := ResamplePCM(data, 16000, 16000)
	if err != nil {
		t.Fatalf("ResamplePCM same rate: %v", err)
	}
	if len(out) != len(data) {
		t.Errorf("expected same length %d, got %d", len(data), len(out))
	}
}

func TestResamplePCM_Upsample(t *testing.T) {
	// 16kHz → 24kHz should produce 1.5x samples.
	numSamples := 1000
	data := makeTestPCM(numSamples)

	out, err := ResamplePCM(data, 16000, 24000)
	if err != nil {
		t.Fatalf("ResamplePCM upsample: %v", err)
	}

	expectedSamples := 1500
	gotSamples := len(out) / PCMBytesPerSample
	if gotSamples != expectedSamples {
		t.Errorf("upsample 16k→24k: got %d samples, want %d", gotSamples, expectedSamples)
	}
}

func TestResamplePCM_Downsample(t *testing.T) {
	// 24kHz → 16kHz should produce 2/3 samples.
	numSamples := 1200
	data := makeTestPCM(numSamples)

	out, err := ResamplePCM(data, 24000, 16000)
	if err != nil {
		t.Fatalf("ResamplePCM downsample: %v", err)
	}

	expectedSamples := 800
	gotSamples := len(out) / PCMBytesPerSample
	if gotSamples != expectedSamples {
		t.Errorf("downsample 24k→16k: got %d samples, want %d", gotSamples, expectedSamples)
	}
}

func TestResamplePCM_Empty(t *testing.T) {
	_, err := ResamplePCM(nil, 16000, 24000)
	if err != ErrEmptyInput {
		t.Errorf("expected ErrEmptyInput, got %v", err)
	}
}

func TestResamplePCM_InvalidRates(t *testing.T) {
	data := makeTestPCM(100)
	_, err := ResamplePCM(data, 0, 16000)
	if err == nil {
		t.Error("expected error for zero fromRate")
	}
}

func TestDefaultCodec(t *testing.T) {
	// This test just verifies the function doesn't panic.
	// Whether a codec is available depends on the system.
	codec, err := DefaultCodec()
	if err != nil {
		t.Logf("no codec available: %v (this is OK in CI without ffmpeg)", err)
		return
	}
	t.Logf("found codec: %s", codec.Name())
	if !codec.Available() {
		t.Error("codec reports available from DefaultCodec but Available() returns false")
	}
}

// makeTestPCM creates synthetic PCM data (sine-like pattern).
func makeTestPCM(numSamples int) []byte {
	data := make([]byte, numSamples*PCMBytesPerSample)
	for i := 0; i < numSamples; i++ {
		// Simple triangle wave for testing.
		val := int16((i % 256) * 128)
		data[2*i] = byte(val)
		data[2*i+1] = byte(val >> 8)
	}
	return data
}
