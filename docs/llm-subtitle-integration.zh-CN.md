# 原生 Go LLM 双语字幕集成与迁移说明

本轮将已验证的 ID 映射试验接入 fusionn 正常队列流程，采用用户确认的 **Go 原生实现**。合并与排版由 Go 完成；可选的词级对齐服务只提供时间点，随 Docker Compose 运行。独立 Python 实验、Gemini/GLM 请求缓存和结果完整保留，便于对照；生产代码不导入 experiments。

当前目标是尽量每屏一行中文、一行英文，同时保留原文、语序和足够的阅读时间。S02E08 已用 `en_chars: 65`、`zh_chars: 20` 完成整集验证，用户肉眼确认效果。原方案中“为解决倒装而整组保持中文”的处理已调整：允许中英片段暂时不完全对应，超限时优先恢复源分段。后续工作见 [TODO](../TODO.md)。

## 使用与切换

完整参数见 [config.example.yaml](../config/config.example.yaml)。新配置默认 `subtitle.engine: llm`；启用字幕时需填写连接和模型。已有配置若含 `subtitle.duosubs` 段而没有 engine，仍选择 DuoSubs；显式 engine（包括环境覆盖）优先。

```yaml
subtitle:
  enabled: true
  engine: llm
  llm:
    base_url: http://host.docker.internal:3425/v1
    model: group/auto-gfmodel
    api_key_env: "" # 可填环境变量名称，例如 SUBTITLE_MODEL_KEY
    api_key: ""     # 可选；优先使用上面的环境引用
    reasoning_effort: "" # 空字符串不发送，不假定各模型支持相同档位
    response_format: json_object
    timeout_seconds: 180
    workers: 3
    retries: 1
    window_seconds: 210
    overlap_seconds: 15
    cache_enabled: true
    cache_dir: /data/llm-subtitle-cache
    en_chars: 65
    zh_chars: 20
    en_gap_ms: 400
    zh_gap_ms: 500
    span_ms: 5500
    fill_gap_ms: 500
    word_align_url: "" # Compose 通过环境变量提供内部服务地址
```

此示例采用 S02E08 已确认的中文上限 20。程序在省略该项时仍默认 `zh_chars: 25`；上限始终读取配置，不会自动从某一集推导。`en_chars`、`zh_chars` 同时参与短句合并与长句候选判断，调整后需重启 fusionn。

主机直接运行时可使用 `http://127.0.0.1:3425/v1`；Docker 内 localhost 指容器本身。Docker Desktop 可使用 `host.docker.internal`，Linux 需配置可达的主机地址或容器网络。不在 URL 中放 userinfo、query 密钥或 fragment，配置会拒绝它们。`api_key_env` 指定但变量未设置时明确报错，不静默退回明文 key；离线 mapping 导入不要求密钥。实际密钥通过部署环境注入，不能写入仓库。

切回 DuoSubs：

```yaml
subtitle:
  engine: duosubs
  duosubs:
    mode: http
    http_url: http://host.docker.internal:8765
    http_container_prefix: /data
    http_host_prefix: /Volumes/External/media
    timeout_minutes: 10
```

只有选中的引擎才初始化和验证依赖。LLM 不需要 DuoSubs/embedding 模型；DuoSubs 不要求 LLM 连接或认证。DuoSubs local/http 两种已有模式保留；当前 fusionn 镜像并不安装本地 duosubs，local 模式需自己提供可执行文件或使用原来的部署。Go LLM 解析/渲染不需要 pysubs2；镜像中的 Python/cleanit 仍用于原有 DuoSubs SDH 清理路径，LLM 模式跳过该清理以保留单边内容。

配置管理器仍会检测文件变化，但服务和队列 worker 在启动时捕获配置。**修改 engine、模型、连接、缓存或样式后需重启 fusionn**；不能把“文件加载成功”理解为运行合并器已热切换。

## 正常生产流程

`NewService → MergerProcessor → StyleProcessor → FontEmbedding → Output → Cleanup` 接入现有合并队列。

1. 分析并提取：LLM 模式用媒体目录中的唯一临时文件，不覆盖同名 sidecar；原生 ASS/SSA 用 copy 保留 an/pos/style，文本轨输出 SRT。失败清理自己创建的临时文件。没有新增 OCR、alass 或 ffsubsync 依赖。
2. 如既有流程要求繁转简且 OpenCC 启用，在私有副本上转换，再构建不可变源库。转换后的文本参与缓存指纹；导入未转换文本的映射会被拒绝，不能混用。停用 OpenCC 时保留原文并记录。
3. Go 解析 raw/plain/display、精确毫秒时间、原生顶部/位置和换行变换记录；模型只返回 ID 组与 layer。严格校验后从本地源库回填文字。
4. 先按已有字符、间隔和跨度条件生成分组事件，再对仍超限的源片段规划时间拆分。需要新增英文句内切点时获取词级时间，不合格则按比例回退；中文优先复用已有切点。最后按英文外框和实际分片计算统一样式、位置与画布。LLM 输出带 `fusionn-llm-styled` 标记，后续 StyleProcessor 不做第二次覆盖。
5. 输出沿用 `.zh-CN.ass` 路径规则，增加源字幕/视频路径相同的保护；清理只处理本次记录的临时路径，持久缓存不会被清理。

请求/超时/取消沿用队列错误处理。任意窗口在有限重试后失败则合并返回错误，不把补回得到的部分字幕伪装为语义成功；取消传播至 HTTP、ffprobe、ffmpeg/OpenCC。局部坏组会隔离、缺失行按原时间单边补回，并保留可观察的拒绝/冲突/补回计数。远端错误只显示状态码和可操作的参数提示，不回显响应正文或密钥。

## 窗口、校验与缓存

- 210 秒 core，前后各 7.5 秒上下文，相邻名义重叠共 15 秒。最早 cue 的 start 决定组归属，完整 cue 不切断。
- 每个模型窗口只接受 `alignments` 数组，每组严格包含 `en_ids`、`zh_ids`、`layer`。拒绝重复 JSON 键、额外文字字段、非整数/bool ID、越界引用、空组、组内/组间重复、原生顶部冲突、逐 cue 对侧距离超过 8 秒或单语组跨度超过 30 秒。跨窗口成员/layer 冲突隔离，漏行单边补回。
- `response_format` 支持 json_object/json_schema/none，本地校验始终执行。历史 Gemini enum 探针没有强约束成功，GLM 的合法输出也不证明服务端 Schema 启用。`reasoning_effort` 留空不发；不支持参数时 HTTP 4xx 会提示检查模型、effort、response_format 与认证，不偷偷换参数重跑。
- 持久请求缓存位于 `cache_dir/subtitlellm-v1/`。key 绑定源指纹、完整 prompt/model/endpoint/request payload 与窗口设置，包含实际发送的 effort，不包含样式或密钥。只改样式可复用；路由别名换底层模型时需清理对应缓存或换目录。
- 开启缓存时源库、完整映射及 audit 另存到 `cache_dir/llm-alignment/<source-prefix>-<mapping-prefix>/sources.json` 和 `alignment/{mapping,audit}.json`，不放随后删除的临时目录。`cache_enabled: false` 时不保存请求或诊断缓存，仍可完成合并。日志保留计数。坏缓存作为 miss 重新请求并记录 `cache_corrupted`；I/O 权限错误明确失败。
- 映射与来源文件可能包含完整字幕原文，缓存目录应仅供运行账号访问。配置日志中的 APIKey、密码和通知 key 已脱敏。

导入既有 Python 或 Go 映射：

```yaml
subtitle:
  llm:
    mapping_path: /path/to/alignment/mapping.json
```

支持 mapping 同目录、上一级或上两级中的 `sources.json`（覆盖生产、Gemini 和 GLM 嵌套实验布局）；要求源指纹、原文、时间和 ID 完整对应、映射指纹一致、组重新校验。遇到存在但不匹配的源文件立即失败，不向上寻找另一个文件绕过。此选项是显式离线导入，若配置模型与映射模型不同，audit 会标记 `model_mismatch` 并记录两者，**实际使用的是映射的原模型**。普通在线缓存仍严格按配置模型隔离。

## 排版与样式

只有 `subtitle.ass_style` 一套共享配置，不增设互相覆盖的 LLM style：

| 参数 | 默认 |
| --- | --- |
| PlayRes | 1920×1080 |
| 中文/英文 | PingFang SC 52 / Helvetica Neue 36 |
| 主/次颜色 | `&H08EFECEA` / `&H087BC0E5` |
| outline / shadow | 1.4 / 1.5 |
| outline/back 颜色 | `&H30141414` / `&H80000000` |
| margin L/R/V | 80 / 80 / 32 |
| bold / scale_x / spacing | false / 100 / 0 |
| line_height_ratio | 1.23 |
| block_padding / collision_gap | 16 / 12 |

默认 `ass_style.enabled: true`，省略字段也真实生效；显式 false/0 不被默认值覆盖。关闭自定义样式时，LLM 仍需基础结构样式供事件引用，使用 Arial/白色等中性外观；DuoSubs/已有双语路径则跳过 StyleProcessor。零边距、零描边、零阴影合法。字体必须存在或通过原 font embedding 功能嵌入；Linux/电视不保证有苹方和 Helvetica，可改用 Noto Sans CJK SC / DejaVu Sans，并挂载对应字体。

普通换行先展平，再按配置中的 `en_chars`、`zh_chars` 判断长度。单条源字幕原本只有一行时，允许超出上限，保留整句，不新增时间切点或强制字符换行，也不请求词级对齐。该例外只认单条源字幕，不能用于多条字幕拼接后的长句。短句继续按原间隔和跨度条件合并；M:N 中文拼接后超限或不满足合并条件时，优先恢复源中文片段，复用有效内部起点，外框仍锁定英文。接受某一片中英文语义顺序不同，不交换或改写源文字。短中文仍可跨英文切片保持。

原本多行的源片段展平后超限时，优先使用原分行位置；原分行不合适时，再从标点、空白附近选择尽量均衡的切点。只有超限的一侧需要拆分。两侧分段数量、显示范围和时间都合适时，共用英文切点；否则中文按自身文字比例计时。总时长足够时，中文比例切点为每段保留最短阅读时间，避免因几十毫秒的不足而改掉原断点。新增片段至少连续显示 1.2 秒；无法合理拆分时保留整句和原换行。原生多人对白、顶部字幕、单边内容和真实重叠继续保留；多人对白需要折行时沿用源说话人断行，避免截断姓名等文字。字符阈值是展示目标，不能保证每条只占两行。ASS 10ms 精度不足以表示某些极短 cue 时会明确报错，避免悄悄丢文字。

S02E08 新版试看使用配置 `en_chars: 65`、`zh_chars: 20`。数值继续读取配置，不按每集自动推导，也不写死在渲染逻辑中。

| 配置 | 当前作用 |
| --- | --- |
| `en_chars` / `zh_chars` | 分别限制短句拼接长度，并筛选需要评估时间拆分的多行源字幕 |
| `en_gap_ms` | 允许拼接英文源片段的相邻间隔上限 |
| `zh_gap_ms` | 允许拼接中文源片段的相邻间隔上限 |
| `span_ms` | 英文合并从第一条开始到最后一条结束的跨度上限；当前中文侧不单独检查该项 |
| `fill_gap_ms` | 保留多个英文分片时，仅补足不超过该值的正间隙，不拼接文字 |

这四个时间参数仍在原分组阶段生效。长句内部的新切点在该阶段之后加入，不再重新合并或补间隙。同一句保持文字在整个显示范围内统一规划，避免每个英文切片都把中文从头拆一次。`span_ms` 不是源单条字幕的最长显示时间。

### Docker 内的词级对齐

仓库的 `docker-compose.yml` 包含 `word-align` 服务，默认使用 4 个 CPU 核和 `Qwen/Qwen3-ForcedAligner-0.6B-hf`，不依赖宿主机 MLX。模型在第一次请求时下载和加载，文件保存在现有 Hugging Face 卷中。首次构建镜像和下载权重的时间不计入每集的推理耗时。

模型在独立工作进程中运行，HTTP 接口保持在线。最后一次对齐请求完成后，连续空闲 300 秒就结束工作进程，释放模型权重、PyTorch 缓存和运行内存；进行中的请求不会被卸载打断。可在 `word-align` 容器中设置 `ALIGN_IDLE_SECONDS` 调整空闲秒数，值必须大于 0。下一次请求会从磁盘缓存重新加载模型，需要支付加载时间，无需重新下载。

```bash
docker compose up -d --build
```

Compose 为主程序设置 `FUSIONN_SUBTITLE_LLM_WORD_ALIGN_URL=http://word-align:8767`。独立运行时可设置 `subtitle.llm.word_align_url`；空字符串使用比例计时。改地址后需重启 fusionn。对齐服务只接收短 WAV 和原英文，不需要媒体目录或宿主机路径映射。

Go 优先选择带英文语言标签的音轨，否则使用第一个音轨；有中央声道时提取中央声道，再转成单声道 16kHz。只对适合拆分的超限英文源片段请求词时间，单条原片段最多 30 秒。请求和音频提取共用每条 60 秒的期限。服务不可用、输出文字不匹配、切点异常或阅读时间不足时，使用比例计时；比例也不合格时保持原事件。对齐请求失败后保留已完成的时间结果，剩余条目回退，不使整集失败；任务取消仍会中止处理。词级时间是模型估计，不能保证每条都比比例自然。

配置中的 `en_chars`、`zh_chars` 同时控制短句合并和长句候选。原字幕本来有两行不会单独触发拆分；首先展平，再比较当前配置阈值。CLI 额外输出 `word-timings.json`，用于核对这次实际采用的词时间。正常服务流程目前不会持久化复用词时间；LLM 配对缓存与词级时间缓存是不同的环节，后者仍列在 [TODO](../TODO.md)。

## 16:9 电视画布与旧配置迁移

旧 384×288 是 ASS 脚本坐标，不能据此推断电视物理分辨率。本次改成 1920×1080 坐标，保留居中 16:9 参考播放画布的意图。

宽片设编码宽高 W/H，参考高 C=W×9/16，缩放 s=max(1,C/H)。字号按 s 放大；1080 脚本坐标中的垂直 margin 用：

```text
margin' = round((margin × C / PlayResY - (C-H)/2) / (H/PlayResY))
```

只在宽片应用，普通 16:9、4:3 保持配置值；未知尺寸安全 fallback。LLM 的显式坐标、整组锚点与行高同步换算，底部字幕可进入播放器新增的黑边，顶部层采用同一参考画布边距；原生 pos 保留其帧内相对位置。StyleProcessor 为 DuoSubs/已有双语保留所有未知样式，定义 Default/Default_1/Eng/TopDefault/TopEng 共享样式，并把旧 PlayRes 的 pos/move 坐标换算到新画布，不直接丢掉原头和样式。

| 编码尺寸 | 字号缩放 | 默认边距 32 换算 |
| --- | ---: | ---: |
| 1920×1080 | 1 | 32 |
| 3840×1920（本集） | 1.125 | −32 |
| 3840×1606（Infuse 宽片样本） | 约 1.345 | −143 |
| 1440×1080（4:3） | 1 | 32 |
| 未知 | 1 | 32 |

负边距与超出编码帧的 pos 是为播放器黑边定位保留的行为；普通 ffmpeg 烧录只裁编码画面，不能代替 Infuse 的 16:9 输出验证。此前意图在几何测试中保留，最终电视观感仍要在目标播放器确认。

旧配置未显式设置 `play_res_y` 时：显式字号/垂直边距/描边/阴影乘新 Y/288（1080 为3.75），左右边距乘新 X/384（1920 为5）；布尔值与零值保留，没写的值使用新默认。旧 20/13/5 会迁移为75/49/19，**不会当成1080p的20/13/5**。复制本轮52/36/32等新值时务必显式写 `play_res_x:1920`、`play_res_y:1080`；也可删掉旧显式数值接受新默认。旧 ScaleX=90 是旧模板硬编码，本次默认100，如要保持压缩外观可显式设90。

## S02E08 当前验证记录

2026-10-21 的新版测试使用完整普通英文 771 条、简中 777 条，配对共 775 组。以既有源轨、配对与此前 Docker CPU 模型的 28 条词时间重新运行完整分段、排版，没有新增 LLM 或词级模型请求。

| 新版 ASS 项目 | 结果 |
| --- | ---: |
| 总事件数 | 820 |
| 单边单行 | 8 |
| 一行中文＋一行英文 | 804 |
| 三行 | 7 |
| 四行 | 1 |

三行和四行全部来自按约定排除时间拆分的多人对白。已验证源文字覆盖、顺序、分组显示时间范围，以及实际输出事件的文字和时间。Mac 与 Linux Docker 输出逐字节一致。行数统计只计算 ASS 明确写入的换行，不能代替播放器实际折行检查。

三个已核验的分段：

| 原片时间 | 中文切换 | 英文显示 |
| --- | --- | --- |
| 04:44.33–04:49.20 | 原两行在 04:46.67 切换 | 沿用已有两段及词级切点 |
| 05:37.88–05:41.34 | 原两行在 05:39.50 切换 | 沿用已有两段及词级切点 |
| 06:20.96–06:24.18 | “如果脉搏开始变弱”与“或者变得细弱难辨 马上告诉我”在 06:22.16 切换 | 整句跨两片保持 |

本机产物在 `/Users/maverick/Desktop/fusionn-subtitle-split-test/full-episode-v2/`：`S02E08.zh20-source-lines.zh-CN.ass`、`README.md`、`validation.json`。用户已肉眼确认新版效果。`make lint` 为 0 问题，`make test` 通过 Go race 测试和 5 项 Python HTTP/生命周期测试。

此前 Docker CPU 基准在 `en_chars: 65`、`zh_chars: 25` 下，音频提取、28 条词级对齐与 ASS 输出合计 33.2 秒。该基准不包含首次模型下载或重新请求 LLM，也不是最新 20 字版的整集测速。模型加载后内存约 4.4GiB，空闲卸载后约 109MiB；重新加载并处理首条短字幕约 5.2 秒，连续下一条约 0.85 秒。测量环境限制 4 核、6GiB。

### 英文长度与配置调整参考

按源文件的普通英文双行台词统计，共 141 条，排除多人对白与顶部内容。字符按 Unicode 计数，包含内部空格、标点；两行之间替换为一个空格，不计算 ASS 格式标签。

| 统计范围 | 最长 | 原片位置 |
| --- | ---: | --- |
| 原英文的每个原始行 | 42 字符 | 多处 |
| 所有普通双行展平后的完整句子 | 84 字符 | 42:04.566–42:08.695 |
| 当前新版实际合成一行显示的英文 | 65 字符 | 42:50.36、45:53.09、52:38.24 |

84 字符的完整句子是 `This makes sense because we were thinking about you when we were trying to get here.`。它已在当前规则下拆分，不是当前画面上的单行长度。

目前仍合成一行的三个最长例子：

| 时间 | 字符数 | 英文 |
| --- | ---: | --- |
| 42:50.36–42:56.12 | 65 | `"Figures out the secrets of the universe at CERN" type physicist.` |
| 45:53.09–45:57.30 | 65 | `We're asking ourselves, "What did you want to do with your life?"` |
| 52:38.24–52:42.62 | 65 | `And I… I gotta live with that, you know? I gotta figure that out.` |

把 `en_chars` 从 65 改为 60，源普通双行中的超限条目会从 28 增至 40；改为 55 则为 54 条。这是长度候选数，最终能否拆分还取决于断点与阅读时长。原本单行的源字幕仍享受超限豁免。

## 本机 Docker Compose 与配置迁移

当前部署目录是 `/Users/maverick/Development/fusionn/`。该目录的 `docker-compose.yml` 已加入 `word-align`，主服务和模型服务都从本机源码构建。主服务镜像名为 `fusionn:local`，模型镜像名为 `fusionn-word-align:local`。此前远程 `ghcr.io/weizsw/fusionn:latest` 不会自动包含尚未发布的工作区代码。备份位于部署目录的 `backups/docker-compose.before-word-align.*.yml`。

新服务复用 `huggingface_cache`，加入现有 `fusionn_default` 网络，使用 4 核、6GiB 和 `ALIGN_IDLE_SECONDS: "300"`。模型 HTTP 端口只在容器网络内使用，不需要映射到宿主机。现有媒体、配置、字体挂载和对外端口保持原值。

实际配置文件是 `configs/fusionn.config.yaml`。在已有 `subtitle.llm` 中将 `zh_chars: 25` 改为 `zh_chars: 20`；`en_chars: 65`、四个时间参数、模型连接和样式继续沿用。Compose 已设置 `FUSIONN_SUBTITLE_LLM_WORD_ALIGN_URL=http://word-align:8767`，无需再往 YAML 加同一个地址。独立运行且不用该环境变量时，才显式配置 `subtitle.llm.word_align_url`。

本次只修改 Compose 并验证构建，没有修改实际配置文件，也没有重启现有服务。改好配置后，在部署目录执行：

```bash
docker compose config --quiet
docker compose up -d --build fusionn word-align
```

这会应用新镜像与配置。以后只调 `en_chars`、`zh_chars` 等已有配置时，执行 `docker compose restart fusionn`，让合并器读取新值。Compose 中的 `build.context` 指向本机 checkout；移动源码目录后需同步路径。正式镜像发布后，可以再切回发布的镜像。

## 分段、上下分区与画面文字避让设计（规则已逐项确认，尚未实现）

以下是长字幕拆分之后的新一轮设计，不代表当前渲染器已经支持这些行为。术语见 [CONTEXT.md](../CONTEXT.md)。

### 普通台词的时间拆分

这里的行数来自源字幕的有效原始分行，忽略格式标签和空行，不采用本程序重新排版产生的断行。双人对白使用下一节的同屏分区规则。

| 原中文 | 原英文 | 处理方式 |
| --- | --- | --- |
| 两行 | 两行 | 优先沿原分行拆成两个连续双语事件，按原顺序一一对应；即使合并后都未超各自配置的字符上限，也不先合并或冻结某一侧 |
| 一行 | 两行 | 先尝试合并英文；不超过 `en_chars` 就合为一行，超过则保留中文整句，让英文分段切换 |
| 两行 | 一行 | 先尝试合并中文；不超过 `zh_chars` 就合为一行，超过则保留英文整句，让中文分段切换 |
| 一行 | 一行 | 保留原单行，沿用原单行可超限的例外 |

两边都有原分段时，配对顺序沿用各语言原文，接受语序差异，不改写或重排文字。新增切换时间仍优先词级对齐，不合格时回退比例计时。两中两英的同步拆分优先于“未超字符上限就合并”和“短侧保持”；同步结果能满足 1.2 秒要求时采用，不满足时先合并两侧的原行，再按长度回退，不直接恢复四行。

| 同步拆分失败后，合并文字的长度 | 回退方向 |
| --- | --- |
| 中英文都未超过各自上限 | 中英各一行，整句显示 |
| 仅中文超过 `zh_chars` | 中文分段切换，英文保持整句 |
| 仅英文超过 `en_chars` | 英文分段切换，中文保持整句 |
| 中英文都超过各自上限 | 继续评估两侧拆分；不能将超长内容当作短侧直接保持 |

1.2 秒只作为原分行中英同步拆分方案的门槛，不作为所有新增片段的统一下限。回退后逐语言规划的片段以 0.8 秒为防快闪下限，一侧变化时另一侧可以保持。若连 0.8 秒也无法满足，就保留完整内容和原显示时间，接受少数多行回退。原字幕本来就短的独立条目保留原时长，不因新下限而被拉长或删除。

计时候选也需要覆盖两中两英的同步拆分，即使英文未超过 `en_chars`。`06:20` 的原中英文都是两行，新规则下优先两边一起切换，取代此前英文整句保持的效果。

### 双人对白的上下分区

- 四行时，按说话人分成两组双语字幕：第一位说话人的中文、英文放上方，第二位的中文、英文放下方。原文和各语言的说话人顺序保持不变。
- 三行时，若其中一种语言已是一行，允许把另一种语言的两行合成一行，即使超过其配置的 `en_chars` 或 `zh_chars`。合并后保留说话人标记，目标是一行中文、一行英文；这项豁免不用于普通长句。
- 三行合并后如果超出实际可用宽度，就恢复原分行，保留原字号。字宽估算纳入 ASS 实际字体、字号、横向缩放、字间距和左右边距；无法可靠判断字宽时保留原分行。
- 四行对白准备分上下时，若另一侧在该段对白期间已有字幕，就保留该段对白的原布局，接受少数四行情况。
- 行数依据输出 ASS 的明确换行。本轮不把所有预估会被播放器自动折行的对白扩大为候选；字宽检查用于保护三行合并，实际播放仍需样片验证。

### 画面文字

- 画面文字指路牌、屏幕消息、时间地点提示等可见文字的翻译；画外音、解说、内心独白都按台词处理。
- 用字幕上下文判断类别，只有明确识别为画面文字时才允许迁移；分类不确定时保留源位置。顶部或斜体不能单独作为画面文字的证据，本轮不加入视频抽帧识别。
- 画面文字与台词在同一侧同时显示，导致字幕堆成多行时，尝试把画面文字移到另一侧。上下两个方向使用相同规则；目标侧已有字幕时保留原位。
- 原本位于同一侧、相邻间隔不超过 500ms 的画面文字组成连续组。相接或重叠也视为连续；间隔更长或源位置发生上下切换时断组，不跨过变侧点强制统一位置。
- 需要迁移时，整组保持同一个目标侧，即使只有其中一段与台词同时出现。检查整组所有片段的显示时段，目标侧必须全程空闲；任一片段会遇到其他字幕，就让整组保持原位置。

### 验收例子

| 情况 | 预期 |
| --- | --- |
| 原中英文都两行，展平后都未超限 | 仍优先沿原分行同步拆成两个双语事件 |
| 原中文一行、英文两行，英文合并后未超限 | 中英各一行，整句显示 |
| 原中文一行、英文两行，英文合并后超限 | 中文保持，英文分段切换 |
| 原中文两行、英文一行，中文合并后未超限 | 中英各一行，整句显示 |
| 原中文两行、英文一行，中文合并后超限 | 英文保持，中文分段切换 |
| 原中英文都两行，但同步拆分不足 1.2 秒，合并后均未超限 | 合并成中英各一行，整句显示 |
| 上例中，仅一侧合并后超限 | 超限侧分段切换，另一侧保持整句；新片段至少显示 0.8 秒 |
| 原单条事件总长 2 秒，仅中文超限，能找到每段 1 秒的切法 | 英文保持、中文切换，不因低于 1.2 秒而拒绝单侧拆分 |
| 回退后的新片段仍不足 0.8 秒且找不到合格切法 | 保留完整内容与原显示时间，允许多行 |
| 四行双人对白，另一侧空闲 | 第一位说话人的一中一英在上，第二位在下 |
| 三行双人对白，合并后可放一行 | 双行语言合成一行，允许超过该语言配置的字符上限 |
| 三行合并后过宽 | 保留原三行与原字号 |
| 四行对白需要的另一侧已有字幕 | 保留原布局 |
| 连续画面文字仅第二段与底部台词重叠，顶部全程空闲 | 整组移到顶部 |
| 上例中，第一段期间顶部已有其他字幕 | 整组保持原位 |
| 原画面文字位置依次为下、上、下 | 在位置变化处断组，不强制统一三段位置 |
| 文字可能是画面内容，也可能是画外音 | 按不确定处理，保持原位 |

本轮只记录设计，尚未修改渲染、分类模型请求、配置或部署。字宽测量、分类结果存储和缓存兼容方式由实现阶段核对，不能将这些规则当作已上线行为。

## 后续 TODO

[TODO.md](../TODO.md) 记录逐条显示决策审计、词级时间持久缓存和跨片源阅读速度验证。本次 GitHub Issue 创建被当前令牌拒绝，因此清单暂存仓库，尚未同步 Issue。当前实现只有固定的最短阅读时长检查，没有按字数执行阅读速度限制。

## 此前集成验证（长字幕拆分前）

以下 720 个事件的记录对应拆分功能加入前的版本。当前输出会增加事件数，验收时检查原文覆盖、顺序和显示时间范围。

已执行 `go test ./...`、`go vet ./...`，以及 config/subtitlellm/service/subtitle 三个包的 `go test -race`。覆盖引擎依赖隔离、旧配置/env/false/0/密钥日志、严格JSON和ID、失败重试与取消、缓存失效/坏缓存恢复、生产映射重导入、M:N/1:N/单边/顶部/重叠、Unicode换行、旧坐标与上述电视尺寸。

实际本集烟测命令（输出目录必须是新的）：

```bash
go run ./cmd/subtitle-merge --config config/config.example.yaml \
  --video '/Volumes/External/media/tv/Dark Matter (2024)/Season 02/Dark Matter (2024) - S02E07 - The Pyramid [ATVP][WEBDL-2160p][EAC3 Atmos 5.1][DV HDR10Plus][h265]-FLUX.mkv' \
  --en-stream 2 --zh-stream 6 \
  --mapping experiments/llm-subtitle-merge/results/dark-matter-s02e07/alignment/mapping.json \
  --out /Users/maverick/.tds/artifacts/llm-subs-dark-matter-s02e07-01a152fd/go-native-integration-final
```

重新 ffprobe/提取完整英文710、简中722条，精确复现原型源指纹；0个模型请求，使用原Gemini映射712组。约5.067秒包含探测与提取，**不是模型耗时**。720个ASS事件逐条对照，纯文本、顺序和起止时间与Python试验全部相同。1920×1080未知尺寸基准测试也验证了事件样式、alignment、x一致；可调行高替代原硬编码64/46后，15个M:N事件 y 有≤1.76像素差异。实际电视适配样例字号59/41、marginV −32，坐标有意区别于未适配Python输出。

本机可播放文件：

```text
/Users/maverick/.tds/artifacts/llm-subs-dark-matter-s02e07-01a152fd/go-native-integration-final/bilingual.ass
```

仓库样例：[Go集成ASS](examples/llm-native-dark-matter/bilingual.ass)、[运行摘要](examples/llm-native-dark-matter/run.json)、[文字时间一致性校验](examples/llm-native-dark-matter/validation.json)。媒体及已有字幕未覆盖。

额外构建了Linux arm64静态二进制，在现有Debian容器中验证启动与离线渲染；无Python合并依赖。Dockerfile同步分发主服务与subtitle-merge命令。未构建/发布完整新镜像，未替换运行容器、合并分支或部署。原型模型性能对比仍见实验报告，本轮没有重新评估模型速度、价格或整集语义准确率。
