package utils

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// WhisperCLITranscriber shells out to the Python `whisper` CLI (openai-whisper).
//
// It is intended for short recordings and returns only text (confidence is not available).
type WhisperCLITranscriber struct {
	cliPath   string
	model     string
	language  string
	extraArgs []string
}

func NewWhisperCLITranscriberFromEnv() *WhisperCLITranscriber {
	cli := strings.TrimSpace(os.Getenv("WHISPER_CLI"))
	if cli == "" {
		cli = "whisper"
	}

	model := strings.TrimSpace(os.Getenv("WHISPER_MODEL"))
	if model == "" {
		model = "base"
	}

	language := strings.TrimSpace(os.Getenv("WHISPER_LANGUAGE"))

	extraArgs := strings.Fields(strings.TrimSpace(os.Getenv("WHISPER_EXTRA_ARGS")))

	return &WhisperCLITranscriber{
		cliPath:   cli,
		model:     model,
		language:  language,
		extraArgs: extraArgs,
	}
}

func (w *WhisperCLITranscriber) CanTranscribe() bool {
	if w.cliPath == "" {
		return false
	}
	_, err := exec.LookPath(w.cliPath)
	return err == nil
}

func (w *WhisperCLITranscriber) TranscribeAudio(ctx context.Context, inputFilePath string) (string, *float32, error) {
	if inputFilePath == "" {
		return "", nil, fmt.Errorf("input file path is empty")
	}

	if _, err := exec.LookPath(w.cliPath); err != nil {
		return "", nil, fmt.Errorf("whisper cli not found (%s): %w", w.cliPath, err)
	}

	// Whisper relies on ffmpeg being available on PATH.
	// On Windows (especially when launched under a debugger), PATH may not include ffmpeg.
	ffmpegBinDir := strings.TrimSpace(os.Getenv("FFMPEG_BIN_DIR"))
	if ffmpegBinDir == "" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return "", nil, fmt.Errorf("ffmpeg not found on PATH; install ffmpeg or set FFMPEG_BIN_DIR")
		}
	}

	outDir, err := os.MkdirTemp("", "talkback_whisper_out_*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp output dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	// Prefer txt output for simplicity.
	// Force fp16 off for broader CPU compatibility.
	args := []string{
		inputFilePath,
		"--model", w.model,
		"--output_dir", outDir,
		"--output_format", "txt",
		"--fp16", "False",
	}

	if w.language != "" {
		args = append(args, "--language", w.language)
	}

	if len(w.extraArgs) > 0 {
		args = append(args, w.extraArgs...)
	}

	cmd := exec.CommandContext(ctx, w.cliPath, args...)
	if ffmpegBinDir != "" {
		// Prepend ffmpeg bin directory to PATH for this process.
		sep := string(os.PathListSeparator)
		cmd.Env = append(os.Environ(), "PATH="+ffmpegBinDir+sep+os.Getenv("PATH"))
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		if errMsg != "" && len(errMsg) > 1000 {
			errMsg = errMsg[:1000] + "..."
		}
		if errMsg != "" {
			return "", nil, fmt.Errorf("whisper cli failed: %s", errMsg)
		}
		return "", nil, fmt.Errorf("whisper cli failed: %w", err)
	}

	// Whisper output location can vary by version; find .txt output recursively.
	// We never rely on the file persisting: outDir is a temp directory removed at the end of this call.
	matches := make([]string, 0, 2)
	_ = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".txt") {
			matches = append(matches, path)
			// Don't bother collecting many.
			if len(matches) >= 3 {
				return fs.SkipAll
			}
		}
		return nil
	})

	// If no .txt was created, some whisper CLI variants print the transcript to stdout.
	// Materialize that into a temp .txt and proceed (still ephemeral).
	if len(matches) == 0 {
		stdoutText := strings.TrimSpace(stdout.String())
		if stdoutText == "" {
			return "", nil, fmt.Errorf("whisper produced no .txt output")
		}
		// Guardrail: sometimes stdout is just a warning (e.g., file missing) rather than a transcript.
		lower := strings.ToLower(stdoutText)
		if strings.Contains(lower, "filenotfounderror") || strings.Contains(lower, "winerror 2") || strings.HasPrefix(lower, "skipping ") {
			return "", nil, fmt.Errorf("whisper did not produce a transcript: %s", truncateForError(stdoutText, 400))
		}
		fallbackPath := filepath.Join(outDir, "transcript.txt")
		if err := os.WriteFile(fallbackPath, []byte(stdoutText), 0644); err != nil {
			return "", nil, fmt.Errorf("failed to write fallback transcript: %w", err)
		}
		matches = append(matches, fallbackPath)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", nil, fmt.Errorf("failed to read whisper output: %w", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", nil, fmt.Errorf("transcription is empty")
	}

	// Python whisper CLI doesn't provide a single confidence score.
	return text, nil, nil
}

func truncateForError(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func VoiceMaxUploadBytesFromEnv() int64 {
	mbStr := strings.TrimSpace(os.Getenv("VOICE_MAX_UPLOAD_MB"))
	if mbStr == "" {
		return 25 * 1024 * 1024
	}
	mb, err := strconv.Atoi(mbStr)
	if err != nil || mb <= 0 {
		return 25 * 1024 * 1024
	}
	return int64(mb) * 1024 * 1024
}

