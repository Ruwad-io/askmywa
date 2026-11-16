# Product demo

The user approved fictional data on 17 November 2026. This replaces the original
request in #26 for recording a live test-account pairing and send. The published
videos are **illustrated presentations of actual local MCP results**, not screen
recordings of Claude and not a demonstration of WhatsApp pairing or delivery.
The synthesis is editorial text written from the returned messages.

## Recorded execution

- Binary: the `askmywa` executable from the public v0.1.0 Darwin ARM64 archive.
- Archive SHA-256: `e7e6232b7335e5ba6bca3213924c96708380fc37056c5408e0b1b2ae19b98d77`.
- Fixture: six fictional messages per language; all dates are 16 November 2026,
  08:02–11:02 Europe/Paris. Contacts use imaginary `999…` identifiers.
- Five real MCP calls per language: find the group, search two matching messages,
  read the six stored messages, resolve Sophie, and request a sending preview.
- `send_message` uses `confirm=false`; the actual result has `sent=false`.
  The script never starts a daemon, pairs a phone or invokes a confirmed send.
- Full structured results and the executable hash are in `demo-run-{fr,en}.json`.

Reproduce the evidence from the repository root:

```sh
python3 .github/scripts/capture-demo.py /path/to/v0.1.0/askmywa
```

The script creates its own temporary archive and removes it afterwards. It never
uses the operator's archive or changes an installed service.

## Rendering

Serve the repository root, then use a browser to render
`.github/assets/demo-video.html?lang=fr&scene=0` at **1280 × 720**. Capture each scene
0–7 for both `fr` and `en` as `{lang}-{scene}.jpg` in an output directory. Wait for
the visible scene text and local fonts before capturing. The source reads the
recorded results; it does not manufacture tool responses. Save each scene's
visible `#caption` text to `captions.json`, shaped as `{"fr": [...], "en": [...]}`.

```sh
python3 .github/scripts/encode-demo.py /path/to/frames
```

FFmpeg encodes two H.264 MP4s with fast start, 1280 × 720, 25 fps, 53.2 seconds,
no audio, and 0.4-second crossfades. Encoder threads are capped at two and filter
threads at one. The script also writes the poster images, WebVTT captions and
`demo-timeline.json`. Browser frames are temporary build inputs, not repository
assets. Only the final media, render source, timing and evidence are committed.

Both landing pages provide a native video player with `preload="none"`, a poster,
an optional caption track and a complete HTML transcript. Captions are also
visible in the video itself. The media has no third-party player or tracking.

Before publishing, check the video decode and duration, original message text,
caption timing, local links, desktop/mobile layout and native browser playback.
