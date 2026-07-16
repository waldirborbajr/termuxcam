package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const captureTimeout = 45 * time.Second

// dHash grid: 9 columns x 8 rows yields 8 horizontal comparisons per row,
// 64 bits total — a standard, well-tested difference-hash size.
const (
	hashGridW = 9
	hashGridH = 8
	hashBits  = 64
)

func acquireWakeLock() {
	if _, err := exec.LookPath("termux-wake-lock"); err == nil {
		exec.Command("termux-wake-lock").Run()
	}
}

func releaseWakeLock() {
	exec.Command("termux-wake-unlock").Run()
}

func capturePhoto(ctx context.Context, hwID, label string) (string, error) {
	ts := time.Now().Format("20060102_150405")
	outfile := filepath.Join(outputDir, fmt.Sprintf("capture_%s_%s.jpg", label, ts))

	cctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "termux-camera-photo", "-c", hwID, outfile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("Capture failed (%s): %s", label, stderr.String()))
		metricsMutex.Lock()
		lastError = stderr.String()
		metricsMutex.Unlock()
		return "", err
	}

	info, err := os.Stat(outfile)
	if err != nil || info.Size() == 0 {
		os.Remove(outfile)
		return "", fmt.Errorf("empty capture")
	}

	metricsMutex.Lock()
	totalCaptures++
	capturesToday++
	metricsMutex.Unlock()

	logMsg(fmt.Sprintf("Captured successfully: %s", outfile))
	return outfile, nil
}

// computeDHash produces a 64-bit difference hash of the image at path, using
// only the stdlib (no external imaging libraries): downsample to a small
// grayscale grid via nearest-neighbor sampling, then encode whether each
// pixel is brighter than its right neighbor.
func computeDHash(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return 0, fmt.Errorf("zero-sized image")
	}

	var grid [hashGridH][hashGridW]uint32
	for gy := 0; gy < hashGridH; gy++ {
		srcY := bounds.Min.Y + gy*h/hashGridH
		for gx := 0; gx < hashGridW; gx++ {
			srcX := bounds.Min.X + gx*w/hashGridW
			gray := color.GrayModel.Convert(img.At(srcX, srcY)).(color.Gray)
			grid[gy][gx] = uint32(gray.Y)
		}
	}

	var hash uint64
	bit := 0
	for gy := 0; gy < hashGridH; gy++ {
		for gx := 0; gx < hashGridW-1; gx++ {
			if grid[gy][gx] > grid[gy][gx+1] {
				hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	return hash, nil
}

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
