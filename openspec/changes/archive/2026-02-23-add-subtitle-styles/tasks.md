## 1. Configuration Schema Updates

- [x] 1.1 Add `margin_left` field to `ASSStyleConfig` struct in `internal/config/config.go` (int, mapstructure tag)
- [x] 1.2 Add `margin_right` field to `ASSStyleConfig` struct in `internal/config/config.go` (int, mapstructure tag)
- [x] 1.3 Add `alignment` field to `ASSStyleConfig` struct in `internal/config/config.go` (int, mapstructure tag)
- [x] 1.4 Add `border_style` field to `ASSStyleConfig` struct in `internal/config/config.go` (int, mapstructure tag)
- [x] 1.5 Add `wrap_style` field to `ASSStyleConfig` struct in `internal/config/config.go` (string, mapstructure tag to handle "0" or 0)

## 2. Configuration File Updates

- [x] 2.1 Add `margin_left: 10` to `ass_style` section in `config/config.example.yaml` with documentation
- [x] 2.2 Add `margin_right: 10` to `ass_style` section in `config/config.example.yaml` with documentation
- [x] 2.3 Add `alignment: 2` to `ass_style` section in `config/config.example.yaml` with documentation explaining numeric alignment values (1-9, numpad layout)
- [x] 2.4 Add `border_style: 1` to `ass_style` section in `config/config.example.yaml` with documentation (1=outline+shadow, 3=opaque box)
- [x] 2.5 Add `wrap_style: 0` to `ass_style` section in `config/config.example.yaml` with documentation (0=smart wrap top wider, 1=end-of-line, 2=no wrap, 3=smart wrap bottom wider)

## 3. Style Generation Updates

- [x] 3.1 Update `generateStyleLine` function in `internal/service/subtitle/style_template.go` to accept and use margin_left, margin_right, alignment, and border_style parameters
- [x] 3.2 Update `generateStyleLine` function to use config values for MarginL, MarginR (replacing hardcoded 10, 10) and Alignment (replacing hardcoded 2)
- [x] 3.3 Update `generateStyleLine` function to use config value for BorderStyle (replacing hardcoded 1)
- [x] 3.4 Update `GetScriptInfo` function in `internal/service/subtitle/style_template.go` to accept wrap_style parameter and generate dynamic WrapStyle value (replacing hardcoded "0")
- [x] 3.5 Update `StyleProcessor.Process` method in `internal/service/subtitle/processor_style.go` to pass wrap_style to GetScriptInfo

## 4. Verification

- [x] 4.1 Run linter to check for errors
- [x] 4.2 Generate a test ASS file and verify new style parameters appear correctly in output
- [x] 4.3 Test configuration hot-reload by modifying values and checking generated styles
