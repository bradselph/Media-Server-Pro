package hls

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"media-server-pro/internal/config"
	"media-server-pro/internal/logger"
)

// detectHWEncoder resolves which video encoder HLS transcodes will use, based
// on the configured hardware_accel preference, and stores it on the module.
// Called once during Start() after ffmpeg has been located.
func (m *Module) detectHWEncoder(cfg *config.Config) {
	choice := strings.ToLower(strings.TrimSpace(cfg.HLS.HardwareAccel))
	if choice == "" {
		choice = "auto"
	}
	enc, dev := resolveHWEncoder(m.ffmpegPath, choice, m.log)
	m.hwEncoder = enc
	m.hwDevice = dev
	if enc == "" {
		m.log.Info("HLS video encoder: software libx264 (hardware_accel=%q)", choice)
	} else {
		m.log.Info("HLS video encoder: hardware %s (hardware_accel=%q)", enc, choice)
	}
}

// hwBackend describes a hardware-encoding backend: its ffmpeg encoder name and,
// for VAAPI, the render device it needs.
type hwBackend struct {
	name    string // config value: nvenc|qsv|vaapi|videotoolbox
	encoder string // ffmpeg encoder: h264_nvenc, h264_qsv, ...
}

// autoProbeOrder is the preference order used when hardware_accel="auto".
var autoProbeOrder = []hwBackend{
	{name: "nvenc", encoder: "h264_nvenc"},
	{name: "qsv", encoder: "h264_qsv"},
	{name: "vaapi", encoder: "h264_vaapi"},
	{name: "videotoolbox", encoder: "h264_videotoolbox"},
}

// resolveHWEncoder picks the ffmpeg video encoder to use for HLS transcoding
// based on the configured choice. It returns the resolved encoder name
// ("h264_nvenc", "h264_vaapi", ...) and, for VAAPI, the render device path.
// An empty encoder means "use software libx264".
//
// Every candidate is verified with a tiny test encode before it is accepted,
// so a host where the encoder is listed but unusable (no GPU, no driver, no
// /dev/dri node) cleanly falls back to software instead of failing every job.
func resolveHWEncoder(ffmpegPath, choice string, log *logger.Logger) (encoder, device string) {
	switch choice {
	case "none":
		return "", ""
	case "nvenc", "qsv", "vaapi", "videotoolbox":
		for _, b := range autoProbeOrder {
			if b.name != choice {
				continue
			}
			if enc, dev, ok := tryHWBackend(ffmpegPath, b, log); ok {
				return enc, dev
			}
			log.Warn("HLS hardware_accel=%q requested but the encoder is not usable on this host; falling back to software libx264", choice)
			return "", ""
		}
		return "", ""
	default: // "auto" (and any unexpected value, which config normalizes to auto)
		for _, b := range autoProbeOrder {
			if enc, dev, ok := tryHWBackend(ffmpegPath, b, log); ok {
				return enc, dev
			}
		}
		return "", ""
	}
}

// tryHWBackend verifies a single backend is both listed by ffmpeg and survives
// a real test encode on this host.
func tryHWBackend(ffmpegPath string, b hwBackend, log *logger.Logger) (encoder, device string, ok bool) {
	var device2 string
	if b.name == "vaapi" {
		device2 = firstVAAPIDevice()
		if device2 == "" {
			log.Debug("HLS VAAPI: no /dev/dri render node found; skipping")
			return "", "", false
		}
	}
	if !encoderListed(ffmpegPath, b.encoder) {
		log.Debug("HLS hardware encoder %s not listed by ffmpeg; skipping", b.encoder)
		return "", "", false
	}
	if !functionalTest(ffmpegPath, b.encoder, device2) {
		log.Debug("HLS hardware encoder %s is listed but failed a test encode; skipping", b.encoder)
		return "", "", false
	}
	return b.encoder, device2, true
}

// encoderListed reports whether ffmpeg advertises the given encoder.
func encoderListed(ffmpegPath, encoder string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-encoders") //nolint:gosec // ffmpegPath is the validated binary path
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), encoder)
}

// functionalTest runs a short encode of a synthetic source through the candidate
// encoder, writing to the null muxer. Success means the encoder actually works
// on this host (GPU present, driver loaded, device accessible) AND survives the
// exact filter chain + rate control the production path uses.
//
// The probe MUST mirror buildVideoEncodeArgs, not a stripped-down command —
// otherwise a probe pass does not guarantee production works, which caused two
// real failures:
//   - VAAPI: production always appends scale_vaapi=W:H, but a probe without it
//     passed on drivers where scale_vaapi is unsupported/broken; every real
//     transcode then failed on the first segment and the job never completed.
//   - NVENC: a 128x72 probe frame is below the encoder's minimum dimension, so a
//     perfectly good GPU was rejected and HLS silently fell back to software.
//
// A realistic 1280x720 source scaled to 640x360 stays above every hardware
// encoder's minimum frame size and exercises the same scaler (CPU scale, or
// GPU scale_vaapi) and bitrate/keyframe args as production.
func functionalTest(ffmpegPath, encoder, device string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error"}
	if device != "" {
		args = append(args, "-vaapi_device", device)
	}
	args = append(args, "-f", "lavfi", "-i", "color=c=black:s=1280x720:r=30:d=0.3")
	if encoder == "h264_vaapi" {
		// Match the production VAAPI filter chain (transcode.go buildVideoEncodeArgs)
		// so a driver that lacks scale_vaapi is rejected here instead of failing
		// every transcode.
		args = append(args, "-vf", "format=nv12,hwupload,scale_vaapi=640:360")
	} else {
		args = append(args, "-vf", "scale=640:360")
	}
	args = append(args,
		"-c:v", encoder,
		"-b:v", "800k", "-maxrate", "800k", "-bufsize", "1600k",
		"-force_key_frames", "expr:gte(t,n_forced*2)",
		"-f", "null", "-",
	)

	cmd := exec.CommandContext(ctx, ffmpegPath, args...) //nolint:gosec // ffmpegPath is the validated binary path; args are constant
	return cmd.Run() == nil
}

// firstVAAPIDevice returns the first existing DRI render node, or "" if none
// (e.g. non-Linux hosts or VPSes without a GPU).
func firstVAAPIDevice() string {
	for _, dev := range []string{
		"/dev/dri/renderD128",
		"/dev/dri/renderD129",
		"/dev/dri/renderD130",
	} {
		if _, err := os.Stat(dev); err == nil {
			return dev
		}
	}
	return ""
}
