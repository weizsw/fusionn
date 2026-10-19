# LLM 编号映射双语字幕合并实验

独立 Python 脚本，不调用或修改 fusionn 生产链。模型只输出 ID 数组与 top/bottom 枚举；字幕文字从本地源数据回填。支持 1:1、N:1、1:N、M:N、单边保留、原生顶部标签、重叠分区与离线重渲染。

本次结果见 [中文实跑报告](REPORT.zh-CN.md)，可直接观看的文件在 [results/dark-matter-s02e07/bilingual.ass](results/dark-matter-s02e07/bilingual.ass)。这是一份试验产物：已按路线 A 废除 M:N 比例分桶，改为整组中文冻帧长驻、英文逐句翻页；组级语义、提前显示及长句遮挡仍需观影判断。

## 环境与运行

Python 3.12（本次 3.12.15）、ffmpeg/ffprobe、pysubs2 1.8.0。依赖固定在 `requirements.txt`。不需要 OpenAI SDK。`alass` 仅在显式传入 `--alass` 时调用。

以下命令在仓库根执行。`extract` 要求新输出目录，ffmpeg 使用 `-n`，不会覆盖源媒体或已有字幕。`align` 会把原文发给指定模型服务，只有这一步需要网络；`render` 完全离线。缓存与排版输出只应写到自己的实验目录。

```bash
uv venv --python 3.12 /tmp/fusionn-llm-subs-venv
uv pip install --python /tmp/fusionn-llm-subs-venv/bin/python -r experiments/llm-subtitle-merge/requirements.txt

PY=/tmp/fusionn-llm-subs-venv/bin/python
SCRIPT=experiments/llm-subtitle-merge/merge.py
OUT=/Users/maverick/.tds/artifacts/llm-subs-dark-matter-s02e07-01a152fd
VIDEO='/Volumes/External/media/tv/Dark Matter (2024)/Season 02/Dark Matter (2024) - S02E07 - The Pyramid [ATVP][WEBDL-2160p][EAC3 Atmos 5.1][DV HDR10Plus][h265]-FLUX.mkv'

# 本次 OUT 已存在；重提取时改成一个新目录。
"$PY" -I "$SCRIPT" extract --video "$VIDEO" --en-stream 2 --zh-stream 6 --out "$OUT"
"$PY" -I "$SCRIPT" align --sources "$OUT/sources.json" --out "$OUT/alignment" \
  --endpoint http://127.0.0.1:3425/v1 --model group/flash-models \
  --response-format json_object --window-seconds 210 --overlap-seconds 15 --workers 3
"$PY" -I "$SCRIPT" render --sources "$OUT/sources.json" \
  --mapping "$OUT/alignment/mapping.json" --out "$OUT/render"
"$PY" -I experiments/llm-subtitle-merge/test_merge.py
```

已提交的源数据与缓存也可直接在另一台机器离线重渲染，不需视频或 API：

```bash
PY=/tmp/fusionn-llm-subs-venv/bin/python
R=experiments/llm-subtitle-merge/results/dark-matter-s02e07
"$PY" -I experiments/llm-subtitle-merge/merge.py render \
  --sources "$R/sources.json" --mapping "$R/alignment/mapping.json" \
  --out /tmp/dark-matter-rerender --en-chars 65 --zh-chars 25
# Linux 缺 PingFang 时，指定播放器已安装的中文字体：
"$PY" -I experiments/llm-subtitle-merge/merge.py render \
  --sources "$R/sources.json" --mapping "$R/alignment/mapping.json" \
  --out /tmp/dark-matter-wqy --zh-font 'WenQuanYi Micro Hei'
```

其他模型服务通过 `--endpoint`（含 `/v1`）、`--model` 指定。若服务需要密钥，用 `--api-key-env VARIABLE_NAME` 读取环境变量，不把密钥写入命令行、缓存或仓库。缓存 key 绑定源数据指纹、完整请求、模型与 endpoint；路由别名将来换后端时须使用新缓存目录。失败请求保存为 `.failed.json`，下次重跑会重试。`initial-run.json` 保存首次运行统计，`run.json` 表示最近一次（包括缓存复跑）。

验证服务端 Schema 支持：

```bash
"$PY" -I experiments/llm-subtitle-merge/probe_schema.py --out /tmp/schema-probe.json
```

本机服务虽然接受 `json_schema`，却返回违反 enum 的值。实跑使用 `json_object` + 本地严格结构校验。单次遵守 Schema 也不能证明服务端强约束。

精确文本的现有 sidecar 对照：

```bash
"$PY" -I experiments/llm-subtitle-merge/compare.py --sources "$OUT/sources.json" \
  --mapping "$OUT/alignment/mapping.json" \
  --sidecar "${VIDEO%.mkv}.zh-CN.ass" --secondary-style Default_1 --out /tmp/comparison.json
```

只有中英各自完整顺序去空白文本与源数据完全相等，比较器才按字符区间恢复 ID 连通组；不使用模糊匹配。不支持字幕内容改变或顺序改变后的猜测。

## 处理规则与边界

- `sources.json` 保存 raw/plain/display、格式变换记录、原时间、预对齐后时间、原样式、位置、多人破折号；SHA-256 指纹检查意外修改，进程内 Cue 不可变。没有翻译、繁简转换或 SDH 删除。
- 每 210 秒为一个 core。上下文在前后各扩 7.5 秒，相邻输入窗口的名义重叠共 15 秒；跨界长 cue 整条送入。组归属为组内最早 cue 的 start 所在 core。同 ID 在不同窗口成员或层级不一致，相关组全部隔离；缺少的 ID 原时间单边补回，绝不用最近时间猜语义。
- 本地拒绝额外字段、非整数（含 bool）、未知/越窗 ID、空组、组内/组间重复、原生 top 冲突、跨语言逐 cue 最小区间距离超 8 秒、单语组跨度超 30 秒。检查覆盖、窗口冲突及源时间；render 再验证完整映射。局部坏组拒绝保留其余组，整个响应非 JSON/结构错误则重试，最终失败退出非零并生成明确的部分结果。
- 配对事件外框以英文为准。短句在英 65 字符、400ms 间隙、5.5s 跨度、中文 25 字符及 500ms 间隙内合并；检测多说话人或后续破折号则不合句。N:1 长句按英文切片，至下一英文的正间隙 ≤500ms 才补齐。1:N 长句借中文起点做内切；非法内切保留所有文字并标记 review。
- 所有 M:N（包括短组）按英文逐句切片，每片保留该组全部中文 ID，按源语序拼接后完整长驻。相邻正间隙 ≤500ms 补齐，长停顿与真实重叠保留。删除比例分桶，不再猜组内短语对应。M:N 中文优先在空白/标点附近均衡换行，预留组内最高字幕块并从其顶部固定中文；`\\q2` 使用脚本显式换行，避免播放器重新平衡。中文在双语块上部，原生 screen top 才放屏幕顶部。所有 M:N 保留 review 标记，整组语义与提前显示仍需抽看。
- 英文普通换行展平，短双人破折号保留并排，过长的源双人行保留分行。渲染按词/字符加展示换行，所有非空白文字逐事件回读核对本地源文本。阈值不承诺两行或像素不溢出。
- 原生 `an`/旧 `a`、ASS 样式 alignment、`pos`（按源 PlayRes 缩放）进入元数据。原生 top 优先，模型可建议 top。不是完整 ASS 特效保真转换器：移动/旋转/卡拉 OK 等特效保存在 raw，不重放；提取 mov_text 等格式也可能损失原位置。此集是 SubRip，仅发现 `an8`，没有 `pos`。
- 真重叠和不确定短重叠均保留原时间，按照全区间保留独立垂直带，显式定位防自动碰撞。没有仅按时长裁剪或擅加说话人横杠。位置与字体行高为估计，复杂重叠仍需播放器检查；过多行导致安全区域不足时明确报错。
- ASS 时间取最近 10ms；原始毫秒时间仍在 sources/events 中。报告中的“不丢文字”指每个源 ID 至少显示一次、渲染非空白字形与本地 display 一致，不包含原始格式码或空白逐字节保真；冻帧可有明确记录的重复显示。

## 产物

`results/dark-matter-s02e07/` 包含源字幕（小文件）、不可变源库、全部 19 份请求/响应缓存、映射、校验、首次/缓存运行摘要、默认 ASS、字体变体、事件来源审计、现有字幕对照和纯背景排版样例（含三段英文翻页前后采样与 freeze-check.json）。最新本机 ASS 位于 `render-chinese-hold/bilingual.ass`，无需再次 align。无视频、模型权重、虚拟环境或私有凭据。完整本机临时产物路径及抽查时间点见报告。
