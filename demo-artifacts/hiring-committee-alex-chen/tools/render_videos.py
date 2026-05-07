"""
Render demo MP4 videos for the hiring-committee-alex-chen dataset.

Implements Path B from generated/mock_video_generation_guide.md:
- Parse transcripts/<round>.transcript.md for [HH:MM:SS] Speaker: turns
- Synthesize per-turn audio with OpenAI TTS, mapping each speaker to a distinct voice
- Concatenate with a small inter-turn gap, mux with the existing thumbnail PNG via ffmpeg
- Emit videos/<round>.mp4 and videos/<round>.vtt

Per-turn audio is cached at /tmp/scrum-326-tts-cache so re-runs resume without paying
again for completed turns.

Run:
    /tmp/demoartifacts-venv/bin/python tools/render_videos.py --only recruiter_screen   # smoke test
    /tmp/demoartifacts-venv/bin/python tools/render_videos.py                            # all seven
    /tmp/demoartifacts-venv/bin/python tools/render_videos.py --mode placeholder         # silent fallback

Notes:
- The candidate, transcripts, and synthesized voices are all fictional. The output
  is for demo purposes only.
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
TRANSCRIPTS = ROOT / "transcripts"
THUMBNAILS = ROOT / "images"
VIDEOS = ROOT / "videos"
CACHE = Path("/tmp/scrum-326-tts-cache")

ROUNDS = [
    "recruiter_screen",
    "hiring_manager_interview",
    "technical_deep_dive",
    "system_design_review",
    "values_behavioral_interview",
    "compensation_leveling_discussion",
    "final_hiring_committee_debrief",
]

# OpenAI TTS provides six voices: alloy, echo, fable, onyx, nova, shimmer.
# Some speakers double up across rounds where overlap won't actually happen
# in a single recording.
VOICE_MAP = {
    "Alex Chen": "onyx",
    "Priya Raman": "nova",
    "Sara Okafor": "shimmer",
    "Diego Alvarez": "echo",
    "Rohan Mehta": "alloy",
    "Nathan Ross": "fable",
    "Marcus Webb": "echo",
    "Emily Torres": "nova",
    "Jenna Liu": "alloy",
}
DEFAULT_VOICE = "alloy"

TURN_RE = re.compile(
    r"\*\*\[(\d+):(\d+):(\d+)\] ([^:]+):\*\*\s*(.+?)(?=\n\n\*\*\[|\n\n---|\Z)",
    re.S,
)

# Stage directions removed before TTS so synthesized speech doesn't read them aloud.
STAGE_DIRECTION_RE = re.compile(r"\[(pause|long pause|longer pause|short pause|laughs|laughter|thinking|sighs|typing|sketching)\]", re.I)


def parse_transcript(path: Path) -> list[dict]:
    text = path.read_text()
    turns = []
    for m in TURN_RE.finditer(text):
        h, mi, s, speaker, line = m.groups()
        ts = int(h) * 3600 + int(mi) * 60 + int(s)
        # collapse whitespace and drop bracketed stage directions for TTS input
        clean = re.sub(r"\s+", " ", line).strip()
        tts_text = STAGE_DIRECTION_RE.sub("", clean).strip()
        tts_text = re.sub(r"\s{2,}", " ", tts_text)
        if not tts_text:
            continue
        turns.append({
            "ts": ts,
            "speaker": speaker.strip(),
            "text": clean,       # kept verbatim for VTT captions
            "tts_text": tts_text,  # cleaned for TTS
        })
    return turns


def load_openai_key() -> None:
    """Pull OPENAI_API_KEY from env, or fall back to /Users/psuthar/code/talkback/.env."""
    if os.environ.get("OPENAI_API_KEY"):
        return
    env_path = Path("/Users/psuthar/code/talkback/.env")
    if env_path.exists():
        for line in env_path.read_text().splitlines():
            line = line.strip()
            if line.startswith("OPENAI_API_KEY="):
                os.environ["OPENAI_API_KEY"] = line.split("=", 1)[1].strip().strip('"').strip("'")
                return
    print("ERROR: OPENAI_API_KEY not set and not found in talkback/.env", file=sys.stderr)
    sys.exit(2)


def synth_turn(client, voice: str, text: str, model: str, cache_path: Path, max_retries: int = 4) -> Path | None:
    if cache_path.exists() and cache_path.stat().st_size > 0:
        return cache_path
    delay = 1.0
    for attempt in range(max_retries):
        try:
            resp = client.audio.speech.create(model=model, voice=voice, input=text[:4000])
            cache_path.parent.mkdir(parents=True, exist_ok=True)
            with open(cache_path, "wb") as f:
                f.write(resp.content)
            return cache_path
        except Exception as e:  # noqa: BLE001 — TTS layer surfaces multiple exception types
            if attempt == max_retries - 1:
                print(f"\n  ! TTS failed after {max_retries} attempts: {e}", file=sys.stderr)
                return None
            print(f"\n  retry {attempt + 1}/{max_retries}: {e}", file=sys.stderr)
            time.sleep(delay)
            delay *= 2
    return None


def build_audio(round_name: str, turns: list[dict], model: str, gap_ms: int = 280):
    from openai import OpenAI
    from pydub import AudioSegment

    client = OpenAI()
    audio = AudioSegment.silent(duration=600)  # leading pad
    cache_dir = CACHE / round_name
    cache_dir.mkdir(parents=True, exist_ok=True)

    char_count = 0
    cache_hits = 0
    new_calls = 0
    for i, turn in enumerate(turns):
        speaker = turn["speaker"]
        voice = VOICE_MAP.get(speaker, DEFAULT_VOICE)
        text = turn["tts_text"]
        if not text:
            continue
        cache_file = cache_dir / f"{i:03d}_{voice}.mp3"
        is_cached = cache_file.exists() and cache_file.stat().st_size > 0
        sys.stdout.write(f"\r  [{i + 1:3d}/{len(turns)}] {speaker[:18]:18s} ({voice:7s}) {len(text):4d} chars  ")
        sys.stdout.flush()
        result = synth_turn(client, voice, text, model, cache_file)
        if result is None:
            audio += AudioSegment.silent(duration=800)
            continue
        if is_cached:
            cache_hits += 1
        else:
            new_calls += 1
            char_count += len(text)
        seg = AudioSegment.from_file(result, format="mp3")
        audio += seg + AudioSegment.silent(duration=gap_ms)
    sys.stdout.write("\n")
    return audio, {"chars": char_count, "cache_hits": cache_hits, "new_calls": new_calls}


def write_vtt(turns: list[dict], vtt_path: Path) -> None:
    lines = ["WEBVTT", ""]
    lines.append("NOTE Synthetic recording. Transcripts and voices are TTS-generated for demo purposes. Candidate is fictional.")
    lines.append("")
    for i, turn in enumerate(turns):
        start = turn["ts"]
        if i + 1 < len(turns):
            end = max(start + 2, min(turns[i + 1]["ts"], start + 14))
        else:
            end = start + 10
        sh, sm, ss = start // 3600, (start % 3600) // 60, start % 60
        eh, em, es = end // 3600, (end % 3600) // 60, end % 60
        speaker = turn["speaker"].replace("[", "(").replace("]", ")")
        text = turn["text"].replace("\n", " ").replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
        if len(text) > 240:
            text = text[:237] + "..."
        lines.append(f"{sh:02d}:{sm:02d}:{ss:02d}.000 --> {eh:02d}:{em:02d}:{es:02d}.000")
        lines.append(f"<v {speaker}>{text}")
        lines.append("")
    vtt_path.write_text("\n".join(lines))


def metadata_duration(round_name: str) -> int | None:
    import json

    path = VIDEOS / f"{round_name}.metadata.json"
    if not path.exists():
        return None
    try:
        return int(json.loads(path.read_text()).get("duration_seconds") or 0) or None
    except Exception:
        return None


def render_one(round_name: str, model: str, mode: str) -> dict:
    transcript_path = TRANSCRIPTS / f"{round_name}.transcript.md"
    thumbnail_path = THUMBNAILS / f"{round_name}_thumbnail.png"
    mp4_path = VIDEOS / f"{round_name}.mp4"
    vtt_path = VIDEOS / f"{round_name}.vtt"

    if not transcript_path.exists():
        print(f"SKIP {round_name}: transcript not found at {transcript_path}", file=sys.stderr)
        return {"round": round_name, "skipped": True}
    if not thumbnail_path.exists():
        print(f"SKIP {round_name}: thumbnail not found at {thumbnail_path}", file=sys.stderr)
        return {"round": round_name, "skipped": True}

    print(f"\n=== {round_name} ===")
    turns = parse_transcript(transcript_path)
    print(f"  {len(turns)} turns parsed")

    write_vtt(turns, vtt_path)
    print(f"  wrote {vtt_path.relative_to(ROOT)}")

    if mode == "placeholder":
        # Silent video; duration = last_ts + 30s, capped to metadata if present.
        dur = max((t["ts"] for t in turns), default=60) + 30
        meta_dur = metadata_duration(round_name)
        if meta_dur:
            dur = max(dur, meta_dur)
        cmd = [
            "ffmpeg", "-y", "-loop", "1", "-i", str(thumbnail_path),
            "-f", "lavfi", "-i", "anullsrc=cl=stereo:r=44100",
            "-t", str(dur),
            "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage",
            "-pix_fmt", "yuv420p",
            "-c:a", "aac", "-b:a", "128k",
            "-shortest", "-movflags", "+faststart",
            str(mp4_path),
        ]
        subprocess.run(cmd, check=True, capture_output=True)
        print(f"  wrote {mp4_path.relative_to(ROOT)} (placeholder/silent, {dur}s)")
        return {"round": round_name, "mode": "placeholder", "duration": dur, "chars": 0}

    audio, stats = build_audio(round_name, turns, model)
    audio_tmp = mp4_path.with_suffix(".tts.mp3")
    audio.export(audio_tmp, format="mp3", bitrate="192k")

    cmd = [
        "ffmpeg", "-y", "-loop", "1", "-i", str(thumbnail_path),
        "-i", str(audio_tmp),
        "-shortest", "-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage",
        "-pix_fmt", "yuv420p",
        "-c:a", "aac", "-b:a", "192k",
        "-movflags", "+faststart",
        str(mp4_path),
    ]
    subprocess.run(cmd, check=True, capture_output=True)
    audio_tmp.unlink(missing_ok=True)

    dur_cmd = [
        "ffprobe", "-v", "quiet",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        str(mp4_path),
    ]
    dur_result = subprocess.run(dur_cmd, capture_output=True, text=True)
    actual_dur = float(dur_result.stdout.strip() or 0)
    meta_dur = metadata_duration(round_name)
    drift = "n/a"
    if meta_dur:
        pct = (actual_dur - meta_dur) / meta_dur * 100
        drift = f"{pct:+.0f}% vs metadata ({meta_dur}s)"

    print(f"  wrote {mp4_path.relative_to(ROOT)}  {actual_dur:.0f}s  {mp4_path.stat().st_size // 1024} KB  drift={drift}")
    print(f"  TTS: {stats['new_calls']} new calls, {stats['cache_hits']} cache hits, {stats['chars']} new chars")
    return {
        "round": round_name,
        "mode": "tts",
        "duration_actual": actual_dur,
        "duration_metadata": meta_dur,
        **stats,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Render demo MP4s from transcripts using OpenAI TTS.")
    parser.add_argument("--only", help="Render only this round (e.g. recruiter_screen)")
    parser.add_argument("--mode", choices=["tts", "placeholder"], default="tts")
    parser.add_argument("--model", default="tts-1", choices=["tts-1", "tts-1-hd"])
    parser.add_argument("--list", action="store_true", help="List round names and exit")
    args = parser.parse_args()

    if args.list:
        for r in ROUNDS:
            print(r)
        return

    if args.only and args.only not in ROUNDS:
        print(f"ERROR: unknown round '{args.only}'. Known: {', '.join(ROUNDS)}", file=sys.stderr)
        sys.exit(2)

    rounds = [args.only] if args.only else ROUNDS
    if args.mode == "tts":
        load_openai_key()
    VIDEOS.mkdir(parents=True, exist_ok=True)

    results = []
    for r in rounds:
        results.append(render_one(r, args.model, args.mode))

    if args.mode == "tts":
        total_chars = sum((r.get("chars") or 0) for r in results)
        cost_per_million = 30.0 if args.model == "tts-1-hd" else 15.0
        est_cost = total_chars * cost_per_million / 1_000_000
        print(f"\nSummary: {len(results)} rounds, {total_chars} new chars synthesized via {args.model}, est cost ${est_cost:.2f}")
    else:
        print(f"\nSummary: {len(results)} rounds rendered as placeholders (silent)")


if __name__ == "__main__":
    main()
