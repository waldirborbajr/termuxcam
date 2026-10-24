# termuxcam TODO

**Current version:** 1.0.0
**Goal:** A resilient, efficient, and observable camera service for Termux, managed through Telegram.

---

## Implemented

- [x] Periodic capture with `termux-camera-photo`
- [x] Telegram photo upload (`sendPhoto`)
- [x] Configuration through `termuxcam.toml`
- [x] In-memory status counters and uptime
- [x] CLI commands for status, configuration, manual capture, and flashlight control
- [ ] Motion detection, heartbeat notifications, and persistent state
- [ ] Telegram chat commands and daily counter reset

---

## Current Priorities

### 1. Stability and resilience (high priority)
- [ ] **Retry queue**: Retry failed photo uploads before capturing a new image.
- [ ] **Automatic disk cleanup**: Rotate or delete old files when `camera_captures` reaches a limit (for example, 2 GB or 7 days).
- [ ] **Configuration hot reload**: Reload `termuxcam.toml` on SIGHUP without restarting the process.
- [ ] **Improved error handling**: Notify Telegram after repeated failures.

### 2. Intelligence and detection (medium/high priority)
- [ ] Advanced event detection (people, animals, broken glass, etc.) with a lightweight model (TFLite or Python + YOLO)
- [ ] Sound detection to trigger a capture
- [ ] Photo notifications with an event description (for example, "Person detected")

### 3. Monitoring and observability
- [ ] Local web dashboard (Go + HTML) with real-time status
- [ ] Tailscale host monitoring with notifications when a host goes offline
- [ ] Structured logging and optional Telegram log delivery
- [ ] Exportable Prometheus metrics

### 4. Quality of life and maintenance
- [ ] Support multiple Telegram chats (groups and users)
- [x] `capture` command for manual photo capture
- [x] `config` command to view and update configuration
- [ ] Automatic versioning and update checks
- [ ] Nix-on-Droid and declarative configuration (SOPS for tokens)
- [ ] Basic unit and integration tests

### 5. Future ideas
- [ ] Low-power mode with longer intervals when no motion is detected
- [ ] Short video recording when motion is detected
- [ ] Home Assistant or MQTT integration
- [ ] Sensor fusion (accelerometer, camera, and sound)
- [ ] Stealth mode (camera LED disabled)

---

## Notes

- **Current priority:** Start with **retry queue + disk cleanup** to improve reliability.
- **Next major technical step:** Integrate a computer vision model.
- **Stability target:** Run for weeks without manual intervention.

---

**Last updated:** October 21, 2026
