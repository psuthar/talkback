# Mock Video Generation Guide

The dataset ships with seven realistic transcripts and metadata stubs but no actual video files. For demos that need real-looking recordings, this guide explains how to turn the transcripts into MP4s without a real recording session.

The demo works *without* video — TalkBack's value here is transcript + multi-artifact reasoning. Generate videos only if the audience explicitly wants the "I'm watching the recording" feel.

---

## Three approaches, ordered by effort

### Approach 1 — Transcript + static thumbnail (no audio, no video)

For most demos, the transcripts and the thumbnail PNGs are enough. The thumbnails already look like Zoom recording cards. TalkBack should treat these as "cited recordings" without playing back.

If your demo UI requires a `.mp4`, generate a one-second placeholder from the thumbnail:

```bash
ffmpeg -loop 1 -i images/recruiter_screen_thumbnail.png -t 1 \
  -vf "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2:black,format=yuv420p" \
  -c:v libx264 -preset veryfast -crf 30 -movflags +faststart \
  videos/recruiter_screen.mp4
```

Repeat per interview. These will satisfy "is there a video file?" checks without any audio.

### Approach 2 — Static thumbnail + synthesized audio voiceover (most demos)

Best ROI: turn each transcript into a voice-over track and overlay on the static thumbnail. Audience hears the conversation, sees a recording-like card.

**Voice synthesis options** (pick what your team uses):
- **ElevenLabs** — natural, cloneable, fastest path to multi-voice. Use distinct voices per speaker (Alex / Priya / Sara / Diego / Rohan / Nathan / Marcus / Emily / Jenna). ~$5–20/month.
- **OpenAI TTS** — six built-in voices. Map: Alex=`onyx`, Priya=`nova`, Sara=`shimmer`, Diego=`echo`, Rohan=`alloy`, Nathan=`onyx` (lower pitch via post), Marcus=`echo`, Emily=`nova`, Jenna=`alloy`. Cheaper and easier to script.
- **Google Cloud TTS WaveNet / Neural2** — strong if you're already on GCP.
- **Apple Say** — instant, free, robotic. Useful for in-house dry runs only.

**Programmatic pipeline (OpenAI TTS, Python sketch):**

```python
# pip install openai pydub
# requires ffmpeg installed
import re, openai, os
from pydub import AudioSegment

speaker_voice = {
    "Alex Chen": "onyx",
    "Priya Raman": "nova",
    "Sara Okafor": "shimmer",
    "Diego Alvarez": "echo",
    "Rohan Mehta": "alloy",
    "Nathan Ross": "onyx",
    "Marcus Webb": "echo",
    "Emily Torres": "nova",
    "Jenna Liu": "alloy",
}

def render(transcript_path, out_mp3):
    text = open(transcript_path).read()
    # Match lines like: **[00:01:48] Alex Chen:** ...
    turns = re.findall(r"\*\*\[\d+:\d+:\d+\] ([^:]+):\*\*\s*(.+?)(?=\n\n|\Z)", text, re.S)
    track = AudioSegment.silent(duration=400)
    for speaker, line in turns:
        speaker = speaker.strip()
        voice = speaker_voice.get(speaker, "alloy")
        line = line.strip().replace("\n", " ")
        if not line: continue
        resp = openai.audio.speech.create(model="tts-1", voice=voice, input=line[:3500])
        with open("/tmp/seg.mp3", "wb") as f:
            f.write(resp.read())
        seg = AudioSegment.from_file("/tmp/seg.mp3", format="mp3")
        track += seg + AudioSegment.silent(duration=300)
    track.export(out_mp3, format="mp3")
```

Then mux audio with the thumbnail to produce a video:

```bash
ffmpeg -loop 1 -i images/recruiter_screen_thumbnail.png \
       -i recruiter_screen.mp3 \
       -shortest -c:v libx264 -tune stillimage -c:a aac -b:a 192k \
       -pix_fmt yuv420p videos/recruiter_screen.mp4
```

**Subtitles:** generate a WebVTT or SRT file from the transcript timestamps so the player can render captions:

```python
import re
with open("transcripts/recruiter_screen.transcript.md") as f:
    text = f.read()
turns = re.findall(r"\*\*\[(\d+:\d+:\d+)\] ([^:]+):\*\*\s*(.+?)(?=\n\n|\Z)", text, re.S)
with open("videos/recruiter_screen.vtt", "w") as out:
    out.write("WEBVTT\n\n")
    for i, (ts, speaker, line) in enumerate(turns):
        h, m, s = (int(x) for x in ts.split(":"))
        start = f"{h:02d}:{m:02d}:{s:02d}.000"
        # crude end time: 8 seconds after start
        end_total = h*3600 + m*60 + s + 8
        eh, em, es = end_total//3600, (end_total%3600)//60, end_total%60
        end = f"{eh:02d}:{em:02d}:{es:02d}.000"
        line = re.sub(r"\s+", " ", line.strip())
        out.write(f"{start} --> {end}\n<v {speaker}>{line}\n\n")
```

### Approach 3 — Generate a fake Zoom/Meet recording UI (highest fidelity)

If the demo audience genuinely wants to watch what looks like a real Zoom recording:

1. Build the speaker tile layout in HTML/CSS or PowerPoint:
   - Top row: speaker tiles with name labels and "muted" indicators
   - Highlight whichever tile is currently speaking with the standard yellow Zoom outline
2. Use the synthesized audio from Approach 2.
3. Use a screen-recorder (OBS, Cleanshot, or Loom) to capture the layout while the audio plays. Drive speaker-highlight transitions from the transcript timestamps using a small JS animation.
4. Fastest path: `manim` or `Remotion` to script the animation programmatically; both can ingest the WebVTT subtitles and switch the active speaker per turn.

**Trade-off:** Approach 3 is high effort. The realism gain is small relative to Approach 2 because audiences engage more with the audio + transcript than with the speaker-tile fidelity.

---

## Recommendations by demo context

| Audience | Recommendation |
|---|---|
| Internal stakeholders, eng/PM | Approach 1 — transcripts only, talk over the screen |
| Sales prospects, design partners | Approach 2 — thumbnail + synthesized audio + captions |
| Executive demo, RFP response, conference | Approach 3 — full Zoom-style fidelity |

## Honest disclaimer to include in the demo

Always tell the audience the recordings are synthetic. Demonstrating realistic-looking interview footage of a non-existent candidate without disclosure is a credibility hazard.

A line that works:

> "These transcripts and recordings are synthetic — fabricated for demo purposes. We're showing how TalkBack would work if these were real interviews."

## Voice fingerprinting note

If you use ElevenLabs voice cloning with someone's actual voice (e.g., a real interviewer on your team consenting to lend their voice for demos), keep the consent record. Don't clone voices of real people who weren't asked.
