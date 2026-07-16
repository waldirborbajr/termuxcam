package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// runOnce captures from every camera selected by cfgSnapshot.CameraMode.
// When force is true (manual /photo request), motion detection is bypassed
// and the frame is always uploaded — but the motion baseline is still
// updated, so the next automatic cycle compares against this fresh frame
// rather than a stale one.
func runOnce(ctx context.Context, cfgSnapshot appConfig, state motionState, force bool) motionState {
	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04:05")

	for _, cam := range camerasForMode(cfgSnapshot.CameraMode) {
		photo, err := capturePhoto(ctx, cam.hwID, cam.label)
		if err != nil {
			continue
		}

		shouldSend := true
		var newHash uint64
		prev, hadPrev := state.Cameras[cam.label]

		if cfgSnapshot.MotionEnabled {
			if h, herr := computeDHash(photo); herr == nil {
				newHash = h
				if hadPrev && !force {
					dist := hammingDistance(h, prev.Hash)
					heartbeatDue := cfgSnapshot.Heartbeat > 0 && !prev.LastSent.IsZero() && now.Sub(prev.LastSent) >= cfgSnapshot.Heartbeat
					if dist < cfgSnapshot.MotionThreshold && !heartbeatDue {
						shouldSend = false
					}
				}
				state.Cameras[cam.label] = camState{Hash: newHash, LastSent: prev.LastSent}
			}
		}

		if !shouldSend {
			os.Remove(photo)
			continue
		}

		suffix := ""
		if force {
			suffix = " (manual)"
		}
		caption := fmt.Sprintf("%s camera: %s%s", strings.Title(cam.label), nowStr, suffix)

		if sendToTelegram(ctx, photo, caption) {
			os.Remove(photo)
			metricsMutex.Lock()
			lastSuccessfulCapture = time.Now()
			metricsMutex.Unlock()
			if cfgSnapshot.MotionEnabled {
				cs := state.Cameras[cam.label]
				cs.LastSent = now
				state.Cameras[cam.label] = cs
			}
		}
	}
	return state
}
