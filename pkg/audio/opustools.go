package audio

import (
	"bytes"
	"fmt"
	"os/exec"
)

// OpusToolsCodec uses opusdec/opusenc (opus-tools package) for conversion.
// Fallback when ffmpeg is not available.
type OpusToolsCodec struct{}

func (c *OpusToolsCodec) Name() string { return "opus-tools" }

func (c *OpusToolsCodec) Available() bool {
	_, err1 := exec.LookPath("opusdec")
	_, err2 := exec.LookPath("opusenc")
	return err1 == nil && err2 == nil
}

// DecodeOGGToPCM converts OGG Opus to raw PCM using opusdec.
// Output: 16-bit LE, mono, at targetSampleRate Hz.
func (c *OpusToolsCodec) DecodeOGGToPCM(oggData []byte, targetSampleRate int) ([]byte, error) {
	if len(oggData) == 0 {
		return nil, ErrEmptyInput
	}

	// opusdec --rate {rate} --force-wav - -
	// Then strip WAV header (44 bytes) to get raw PCM.
	// Alternative: use --raw flag for raw output.
	cmd := exec.Command("opusdec",
		"--rate", fmt.Sprintf("%d", targetSampleRate),
		"--raw",
		"--raw-bits", "16",
		"--raw-rate", fmt.Sprintf("%d", targetSampleRate),
		"--raw-chan", "1",
		"--raw-endianness", "0", // Little-endian
		"-", "-",
	)

	cmd.Stdin = bytes.NewReader(oggData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: opusdec: %s (%v)", ErrDecodeFailed, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

// EncodePCMToOGG converts raw PCM to OGG Opus using opusenc.
// Input: 16-bit LE, mono, at sampleRate Hz.
func (c *OpusToolsCodec) EncodePCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	if len(pcmData) == 0 {
		return nil, ErrEmptyInput
	}

	// opusenc --raw --raw-bits 16 --raw-rate {rate} --raw-chan 1
	//         --raw-endianness 0 --bitrate 32 - -
	cmd := exec.Command("opusenc",
		"--raw",
		"--raw-bits", "16",
		"--raw-rate", fmt.Sprintf("%d", sampleRate),
		"--raw-chan", "1",
		"--raw-endianness", "0",
		"--bitrate", "32",
		"--quiet",
		"-", "-",
	)

	cmd.Stdin = bytes.NewReader(pcmData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: opusenc: %s (%v)", ErrEncodeFailed, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}
