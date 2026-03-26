package audio

import (
	"bytes"
	"fmt"
	"os/exec"
)

// FFmpegCodec uses ffmpeg for audio conversion.
// Most capable: handles any format ffmpeg supports.
type FFmpegCodec struct{}

func (c *FFmpegCodec) Name() string { return "ffmpeg" }

func (c *FFmpegCodec) Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// DecodeOGGToPCM converts OGG Opus to raw PCM using ffmpeg.
// Output: 16-bit LE, mono, at targetSampleRate Hz.
func (c *FFmpegCodec) DecodeOGGToPCM(oggData []byte, targetSampleRate int) ([]byte, error) {
	if len(oggData) == 0 {
		return nil, ErrEmptyInput
	}

	// ffmpeg -i pipe:0 -f s16le -acodec pcm_s16le -ar {rate} -ac 1 pipe:1
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", fmt.Sprintf("%d", targetSampleRate),
		"-ac", "1",
		"pipe:1",
	)

	cmd.Stdin = bytes.NewReader(oggData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: ffmpeg: %s (%v)", ErrDecodeFailed, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

// EncodePCMToOGG converts raw PCM to OGG Opus using ffmpeg.
// Input: 16-bit LE, mono, at sampleRate Hz.
func (c *FFmpegCodec) EncodePCMToOGG(pcmData []byte, sampleRate int) ([]byte, error) {
	if len(pcmData) == 0 {
		return nil, ErrEmptyInput
	}

	// ffmpeg -f s16le -ar {rate} -ac 1 -i pipe:0 -c:a libopus -b:a 32k -f ogg pipe:1
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", "1",
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-b:a", "32k",
		"-f", "ogg",
		"pipe:1",
	)

	cmd.Stdin = bytes.NewReader(pcmData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: ffmpeg: %s (%v)", ErrEncodeFailed, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}
