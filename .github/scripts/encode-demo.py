#!/usr/bin/env python3
"""Encode the 1280×720 browser-rendered demo frames with FFmpeg (two threads).

Usage: python3 .github/scripts/encode-demo.py /path/to/frames
Expected inputs: {fr,en}-{0..7}.jpg and captions.json from demo-video.html.
"""
import json
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
DURATIONS = [5, 7, 8, 8, 8, 8, 7, 5]
FADE = 0.4
FPS = 25  # Every scene duration and crossfade falls exactly on a frame.


def clock(seconds):
    ms = round(seconds * 1000)
    return f"{ms//3600000:02}:{ms//60000%60:02}:{ms//1000%60:02}.{ms%1000:03}"


def main():
    frames = Path(sys.argv[1]).resolve()
    assets = ROOT / "site/assets"
    captions = json.loads((frames / "captions.json").read_text())
    timeline = {"duration_seconds": sum(DURATIONS) - FADE * (len(DURATIONS) - 1), "fps": FPS, "scenes": []}
    start = 0
    for i, duration in enumerate(DURATIONS):
        timeline["scenes"].append({"scene": i, "start": round(start, 2), "end": round(start + duration, 2), "captions": {lang: captions[lang][i] for lang in ("fr", "en")}})
        start += duration - FADE
    # Crossfades use the incoming scene's caption from the beginning of the fade.
    for i, scene in enumerate(timeline["scenes"][:-1]):
        scene["end"] = timeline["scenes"][i + 1]["start"]
    for language in ("fr", "en"):
        command = ["ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-filter_complex_threads", "1"]
        for i, duration in enumerate(DURATIONS):
            command += ["-loop", "1", "-framerate", str(FPS), "-t", str(duration), "-i", str(frames / f"{language}-{i}.jpg")]
        filters = [f"[{i}:v]format=yuv420p,setsar=1,settb=AVTB[v{i}]" for i in range(len(DURATIONS))]
        elapsed = DURATIONS[0]
        previous = "v0"
        for i in range(1, len(DURATIONS)):
            offset = elapsed - FADE * i
            output = f"fade{i}"
            filters.append(f"[{previous}][v{i}]xfade=transition=fade:duration={FADE}:offset={offset:.2f}[{output}]")
            previous = output
            elapsed += DURATIONS[i]
        filters.append(f"[{previous}]scale=in_range=pc:out_range=tv:out_color_matrix=bt709,format=yuv420p[vout]")
        command += ["-filter_complex", ";".join(filters), "-map", "[vout]", "-an", "-c:v", "libx264", "-threads", "2",
                    "-preset", "medium", "-crf", "22", "-g", "50", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
                    "-color_range", "tv", "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709",
                    "-t", str(timeline["duration_seconds"]), str(assets / f"demo-{language}.mp4")]
        subprocess.run(command, check=True)
        shutil.copyfile(frames / f"{language}-0.jpg", assets / f"demo-{language}.jpg")
        cues = ["WEBVTT", ""]
        for scene in timeline["scenes"]:
            cues += [str(scene["scene"] + 1), f"{clock(scene['start'])} --> {clock(scene['end'])}", scene["captions"][language], ""]
        (assets / f"demo-{language}.vtt").write_text("\n".join(cues) + "\n")
        print(f"Encoded {language}: {timeline['duration_seconds']:.1f} seconds", flush=True)
    (ROOT / ".github/assets/demo-timeline.json").write_text(json.dumps(timeline, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
