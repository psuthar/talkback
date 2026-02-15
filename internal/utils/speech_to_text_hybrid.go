package utils

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrAudioTooLong is returned when audio exceeds STT_MAX_AUDIO_SECONDS (handler should return 413).
var ErrAudioTooLong = fmt.Errorf("audio exceeds maximum allowed duration")

// ErrDailyCapExceeded is returned when STT_DAILY_MAX_SECONDS is exceeded (handler should return 429).
var ErrDailyCapExceeded = fmt.Errorf("daily speech-to-text cap exceeded")

// STT mode: "api" = only OpenAI API, "cli" = only Whisper CLI, "hybrid" = try API then CLI.
const (
	STTModeAPI    = "api"
	STTModeCLI    = "cli"
	STTModeHybrid = "hybrid"
)

type sttConfig struct {
	mode           string
	preferCLI      bool
	timeoutMs      int
	maxAudioSec    int
	apiModel       string
	cliModel       string
	costPerMinUSD  float64
	dailyMaxSec    int
}

func getSTTConfig() sttConfig {
	c := sttConfig{
		mode:          strings.TrimSpace(strings.ToLower(os.Getenv("STT_MODE"))),
		timeoutMs:     15000,
		maxAudioSec:   30,
		apiModel:      "whisper-1",
		cliModel:      "tiny",
		costPerMinUSD: 0.006, // conservative default; override with STT_COST_PER_MIN_USD
		dailyMaxSec:   0,     // 0 = no cap
	}
	if c.mode == "" {
		c.mode = STTModeHybrid
	}
	if os.Getenv("STT_PREFER_CLI") == "true" || os.Getenv("STT_PREFER_CLI") == "1" {
		c.preferCLI = true
	}
	if v := strings.TrimSpace(os.Getenv("STT_TIMEOUT_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			c.timeoutMs = ms
		}
	}
	if v := strings.TrimSpace(os.Getenv("STT_MAX_AUDIO_SECONDS")); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			c.maxAudioSec = s
		}
	}
	if v := strings.TrimSpace(os.Getenv("STT_API_MODEL")); v != "" {
		c.apiModel = v
	}
	if v := strings.TrimSpace(os.Getenv("STT_CLI_MODEL")); v != "" {
		c.cliModel = v
	}
	if v := strings.TrimSpace(os.Getenv("STT_COST_PER_MIN_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			c.costPerMinUSD = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("STT_DAILY_MAX_SECONDS")); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			c.dailyMaxSec = s
		}
	}
	return c
}

// EstimateAudioDurationSeconds returns approximate duration in seconds from a WAV file header.
// Returns (0, false) for non-WAV or unreadable (caller may allow and log "unknown").
func EstimateAudioDurationSeconds(filePath string) (float64, bool) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	buf := make([]byte, 44)
	if _, err := f.Read(buf); err != nil {
		return 0, false
	}
	if string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		return 0, false
	}
	byteRate := binary.LittleEndian.Uint32(buf[28:32])
	if byteRate == 0 {
		return 0, false
	}
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	// Walk chunks to find "data"
	pos := int64(12)
	chunkBuf := make([]byte, 8)
	for pos+8 <= info.Size() {
		if _, err := f.ReadAt(chunkBuf, pos); err != nil {
			break
		}
		chunkID := string(chunkBuf[0:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkBuf[4:8]))
		if chunkID == "data" {
			sec := float64(chunkSize) / float64(byteRate)
			if sec > 0 && sec <= 24*3600 {
				return sec, true
			}
			return 0, false
		}
		pos += 8 + chunkSize
	}
	// Fallback: rough estimate from file size (assume most of file is PCM data)
	sec := float64(info.Size()) / float64(byteRate)
	if sec <= 0 || sec > 24*3600 {
		return 0, false
	}
	return sec, true
}

var (
	dailyCapMu   sync.Mutex
	dailyCapDate string
	dailyCapSec  float64
)

func checkAndAddDailyCap(seconds float64, maxSec int) error {
	if maxSec <= 0 {
		return nil
	}
	dailyCapMu.Lock()
	defer dailyCapMu.Unlock()
	today := time.Now().Format("2006-01-02")
	if dailyCapDate != today {
		dailyCapDate = today
		dailyCapSec = 0
	}
	if dailyCapSec+seconds > float64(maxSec) {
		return ErrDailyCapExceeded
	}
	dailyCapSec += seconds
	return nil
}

// HybridSpeechToText prefers OpenAI Whisper API when configured; falls back to Whisper CLI.
type HybridSpeechToText struct {
	cfg         sttConfig
	api         *WhisperTranscriber
	cli         *WhisperCLITranscriber
	apiTimeout  time.Duration
}

// NewHybridSpeechToText returns a hybrid STT using env config.
func NewHybridSpeechToText() *HybridSpeechToText {
	cfg := getSTTConfig()
	apiForSTT := NewWhisperTranscriberForSTT(cfg.apiModel)
	cli := NewWhisperCLITranscriberForSTT(cfg.cliModel)
	return &HybridSpeechToText{
		cfg:        cfg,
		api:        apiForSTT,
		cli:        cli,
		apiTimeout: time.Duration(cfg.timeoutMs) * time.Millisecond,
	}
}

// CanTranscribe returns true if at least one backend (API or CLI) is available.
func (h *HybridSpeechToText) CanTranscribe() bool {
	switch h.cfg.mode {
	case STTModeAPI:
		return h.api.CanTranscribe()
	case STTModeCLI:
		return h.cli.CanTranscribe()
	default:
		return h.api.CanTranscribe() || h.cli.CanTranscribe()
	}
}

// TranscribeAudio runs STT with guardrails, observability, and API-first fallback.
func (h *HybridSpeechToText) TranscribeAudio(ctx context.Context, inputFilePath string) (string, *float32, error) {
	audioSec, ok := EstimateAudioDurationSeconds(inputFilePath)
	if ok && float64(h.cfg.maxAudioSec) > 0 && audioSec > float64(h.cfg.maxAudioSec) {
		return "", nil, ErrAudioTooLong
	}
	if !ok {
		audioSec = 0
	}

	reserveSec := audioSec
	if !ok && reserveSec <= 0 && h.cfg.dailyMaxSec > 0 {
		reserveSec = float64(h.cfg.maxAudioSec) // reserve max when duration unknown
	}
	if err := checkAndAddDailyCap(reserveSec, h.cfg.dailyMaxSec); err != nil {
		return "", nil, err
	}

	start := time.Now()
	var provider string
	var text string
	var confidence *float32
	var err error

	useAPI := (h.cfg.mode == STTModeAPI || (h.cfg.mode == STTModeHybrid && !h.cfg.preferCLI && h.api.CanTranscribe()))
	useCLI := (h.cfg.mode == STTModeCLI || (h.cfg.mode == STTModeHybrid && (h.cfg.preferCLI || !h.api.CanTranscribe())))

	if useAPI && h.api.CanTranscribe() {
		ctx2, cancel := context.WithTimeout(ctx, h.apiTimeout)
		defer cancel()
		text, confidence, err = h.api.TranscribeAudio(ctx2, inputFilePath)
		provider = "api"
		if err != nil && isRetryableSTTError(err) && useCLI && h.cli.CanTranscribe() {
			provider = "cli"
			text, confidence, err = h.cli.TranscribeAudio(ctx, inputFilePath)
		}
	} else if useCLI && h.cli.CanTranscribe() {
		provider = "cli"
		text, confidence, err = h.cli.TranscribeAudio(ctx, inputFilePath)
	} else {
		err = fmt.Errorf("no STT backend available (api=%v cli=%v)", h.api.CanTranscribe(), h.cli.CanTranscribe())
	}

	totalMs := time.Since(start).Milliseconds()
	audioMin := audioSec / 60
	if !ok {
		audioMin = 0
	}
	costUSD := 0.0
	if provider == "api" && audioMin > 0 && h.cfg.costPerMinUSD > 0 {
		costUSD = audioMin * h.cfg.costPerMinUSD
	}
	status := "success"
	errClass := ""
	if err != nil {
		status = "failure"
		errClass = classifySTTError(err)
	}
	log.Printf("stt provider=%s audio_seconds=%.1f total_ms=%d status=%s error_class=%s estimated_stt_cost_usd=%.4f",
		provider, audioSec, totalMs, status, errClass, costUSD)
	if err != nil {
		return "", nil, err
	}
	return text, confidence, nil
}

func isRetryableSTTError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "timeout") || strings.Contains(s, "429") || strings.Contains(s, "503") ||
		strings.Contains(s, "502") || strings.Contains(s, "5")
}

func classifySTTError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "timeout"):
		return "timeout"
	case strings.Contains(s, "401") || strings.Contains(s, "unauthorized"):
		return "401"
	case strings.Contains(s, "429"):
		return "429"
	case strings.Contains(s, "503"), strings.Contains(s, "502"), strings.Contains(s, "500"):
		return "5xx"
	case strings.Contains(s, "whisper cli not found"), strings.Contains(s, "ffmpeg not found"):
		return "cli_missing"
	default:
		return "other"
	}
}
