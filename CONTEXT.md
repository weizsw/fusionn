# Fusionn

Fusionn creates bilingual subtitle files for media managed by Sonarr and Radarr.

## Language

**Reference playback canvas**:
The centered 16:9 output canvas assumed when actual playback display dimensions are unavailable.
_Avoid_: Encoded video frame, detected display

**Consistent subtitle placement**:
The same proportional distance above the playback output canvas's bottom edge, including player-added letterbox bars, regardless of encoded video resolution or aspect ratio.
_Avoid_: Encoded-frame-relative placement, fixed source-pixel offset

**Consistent subtitle scale**:
The same proportional glyph height relative to the playback output canvas, including player-added letterbox bars, regardless of encoded video resolution or aspect ratio.
_Avoid_: Encoded-frame-relative font size, fixed rendered-pixel size

**Encoded letterbox bar**:
Black pixels contained within the encoded video frame.
_Avoid_: Baked-in black bar

**Player-added letterbox bar**:
Blank output-canvas space added outside the encoded video frame by the playback system to preserve aspect ratio.
_Avoid_: Encoded letterbox bar
