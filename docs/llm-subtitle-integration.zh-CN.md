# 原生 Go LLM 双语字幕集成与迁移说明

本轮将已验证的 ID 映射试验接入 fusionn 正常队列流程，采用用户确认的 **Go 原生实现**。不运行 Python 合并程序，也不增加常驻服务。独立 Python 实验、Gemini/GLM 请求缓存和结果完整保留，便于对照；生产代码不导入 experiments。

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
```

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
4. 按英文外框渲染，应用统一样式/画布；LLM 输出带 `fusionn-llm-styled` 标记，后续 StyleProcessor 保持其样式与绝对坐标，不做第二次覆盖。
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

路线 A：所有 M:N 按源中文语序合并整句，每个英文切片显示完整中文；按组内最高块预留空间、顶部锚定中文，英文行数改变不使中文跳动。中文在双语块上方，只有原生/明确 top 层放屏幕顶部。长中文优先标点/空白附近均衡换行，允许三行或更多；仍需观影判断遮挡及提前显示。≤500ms 正间隙补齐，长停顿和真实重叠保留。

1:N 外框锁英文、内切借中文起点；内部点非法时完整保留并标记 review。N:1 短句可合并，长句按英文切片、中文长驻。单边全部保留；原生多人破折号与顶部位置保留，重叠安排独立垂直带，不因时长短就裁掉声音。字符阈值仍是展示起点，不能保证每条只占两行。ASS 10ms 精度不足以表示某些极短 cue 时会明确报错，避免悄悄丢文字。

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

## 验证及本集集成烟测

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
