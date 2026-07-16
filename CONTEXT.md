# Fusionn

Fusionn creates bilingual subtitle files for media managed by Sonarr and Radarr.

## Language

**Consistent subtitle placement**:
The same proportional distance above the playback output canvas's bottom edge, including player-added letterbox bars, regardless of encoded video resolution or aspect ratio.
_Avoid_: Encoded-frame-relative placement, fixed source-pixel offset

**Encoded letterbox bar**:
Black pixels contained within the encoded video frame.
_Avoid_: Baked-in black bar

**Player-added letterbox bar**:
Blank output-canvas space added outside the encoded video frame by the playback system to preserve aspect ratio.
_Avoid_: Encoded letterbox bar
