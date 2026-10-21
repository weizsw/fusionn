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

**普通字幕**:
同一条字幕呈现一个说话人的一段连续台词。原文可以是一行或多行。

**对白字幕**:
同一条字幕分列两位说话人的台词，通常以 `-` 等说话人标记区分。这里也称双人对白。

**源位置**:
源字幕指定的屏幕位置，不包含输出时为了避让其他字幕而产生的位置调整。

**台词**:
人声所表达内容的字幕，包括对白、画外音、解说和内心独白。

**画外音**:
当前画面中看不到说话人的人声，包括解说、内心独白和电话另一端的人声。画外音描述声音来源，可以与普通字幕或对白字幕重叠。

**画面文字**:
画面中可见文字的翻译字幕，例如路牌、屏幕消息和时间地点提示。
_Avoid_: 旁白（容易与人声解说混淆）

**连续画面文字组**:
时间上连续、源位置处于同一侧的一组画面文字。

**时间拆分**:
将同一段字幕内容分成前后连续显示的多个片段。

**保持显示（冻结）**:
一种语言的完整文字跨过另一种语言的片段切换，持续显示。
