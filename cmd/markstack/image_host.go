package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/imagehost"
)

var (
	settingsImageHostID    = controlTree.ID(page, "settings-image-host")
	imageHostLocalID       = controlTree.ID(page, "image-host-local")
	imageHostHTTPID        = controlTree.ID(page, "image-host-http")
	imageHostS3ID          = controlTree.ID(page, "image-host-s3")
	imageHostModePostID    = controlTree.ID(page, "image-host-method-post")
	imageHostModePutID     = controlTree.ID(page, "image-host-method-put")
	imageHostPathStyleID   = controlTree.ID(page, "image-host-path-style")
	imageHostSaveID        = controlTree.ID(page, "image-host-save")
	imageHostTestID        = controlTree.ID(page, "image-host-test")
	imageHostHTTPEndpoint  = controlTree.ID(page, "image-host-http-endpoint")
	imageHostHTTPFileField = controlTree.ID(page, "image-host-http-file-field")
	imageHostHTTPJSONPath  = controlTree.ID(page, "image-host-http-json-path")
	imageHostHTTPHeaders   = controlTree.ID(page, "image-host-http-headers")
	imageHostS3Endpoint    = controlTree.ID(page, "image-host-s3-endpoint")
	imageHostS3Region      = controlTree.ID(page, "image-host-s3-region")
	imageHostS3Bucket      = controlTree.ID(page, "image-host-s3-bucket")
	imageHostS3AccessKey   = controlTree.ID(page, "image-host-s3-access-key")
	imageHostS3Secret      = controlTree.ID(page, "image-host-s3-secret")
	imageHostS3Prefix      = controlTree.ID(page, "image-host-s3-prefix")
	imageHostS3PublicBase  = controlTree.ID(page, "image-host-s3-public-base")
)

type imageHostState struct {
	cfg     imagehost.Config
	store   *imagehost.ConfigStore
	loadErr error
	form    *imageHostForm
	upload  func(context.Context, imagehost.Config, string, []byte, string) (string, error)
	test    func(context.Context, imagehost.Config) error
	paste   *pendingImagePaste
}

type imageHostForm struct {
	mode      imagehost.Mode
	method    string
	pathStyle bool
	fields    map[gfx.ID]*app.TextInput
	feedback  string
	scan      panelScan[struct{}]
}

var (
	errImageHostHeaders = errors.New("custom headers must be a JSON object with string values")
	errImageHostIME     = errors.New("finish composing text first")
)

func (state *model) loadImageHost() {
	if os.Getenv("VIVA_HEADLESS") == "1" && os.Getenv("MARKSTACK_IMAGEHOST_PATH") == "" {
		return
	}
	store, err := imagehost.DefaultStore()
	if err == nil {
		err = state.enableImageHost(store)
	}
	state.imageHost.loadErr = err
}

func (state *model) enableImageHost(store imagehost.ConfigStore) error {
	state.imageHost.store = &store
	state.imageHost.cfg, state.imageHost.loadErr = store.Load()
	return state.imageHost.loadErr
}

func (state *model) initImageHostForm() {
	cfg := state.imageHost.cfg
	if cfg.Mode == "" {
		cfg = imagehost.DefaultConfig()
	}
	headers := ""
	if len(cfg.HTTP.Headers) > 0 {
		data, _ := json.Marshal(cfg.HTTP.Headers)
		headers = string(data)
	}
	form := &imageHostForm{mode: cfg.Mode, method: cfg.HTTP.Method, pathStyle: cfg.S3.PathStyle, fields: make(map[gfx.ID]*app.TextInput)}
	for id, value := range map[gfx.ID]string{
		imageHostHTTPEndpoint: cfg.HTTP.Endpoint, imageHostHTTPFileField: cfg.HTTP.FileField,
		imageHostHTTPJSONPath: cfg.HTTP.JSONPath, imageHostHTTPHeaders: headers,
		imageHostS3Endpoint: cfg.S3.Endpoint, imageHostS3Region: cfg.S3.Region,
		imageHostS3Bucket: cfg.S3.Bucket, imageHostS3AccessKey: cfg.S3.AccessKeyID,
		imageHostS3Secret: cfg.S3.SecretAccessKey, imageHostS3Prefix: cfg.S3.PathPrefix,
		imageHostS3PublicBase: cfg.S3.PublicBaseURL,
	} {
		form.fields[id] = &app.TextInput{Editor: text.NewEditor(value)}
	}
	if err := state.imageHost.loadErr; err != nil {
		form.feedback = state.imageHostErrorText(err)
	}
	state.imageHost.form = form
}

func (host *imageHostState) closeForm() {
	if host.form != nil {
		host.form.scan.stop()
		host.form = nil
	}
}

func (form *imageHostForm) configuration(saved imagehost.Config) (imagehost.Config, error) {
	if saved.Mode == "" {
		saved = imagehost.DefaultConfig()
	}
	for _, field := range form.fields {
		if _, composing := field.Editor.Composition(); composing {
			return imagehost.Config{}, errImageHostIME
		}
	}
	value := func(id gfx.ID) string { return strings.TrimSpace(form.fields[id].Editor.CommittedString()) }
	saved.Mode = form.mode
	switch form.mode {
	case imagehost.ModeHTTP:
		saved.HTTP = imagehost.HTTPConfig{Endpoint: value(imageHostHTTPEndpoint), Method: form.method, FileField: value(imageHostHTTPFileField), JSONPath: value(imageHostHTTPJSONPath)}
		if input := value(imageHostHTTPHeaders); input != "" {
			if err := json.Unmarshal([]byte(input), &saved.HTTP.Headers); err != nil || saved.HTTP.Headers == nil {
				return imagehost.Config{}, errImageHostHeaders
			}
		}
	case imagehost.ModeS3:
		saved.S3 = imagehost.S3Config{
			Endpoint: value(imageHostS3Endpoint), Region: value(imageHostS3Region), Bucket: value(imageHostS3Bucket),
			AccessKeyID: value(imageHostS3AccessKey), SecretAccessKey: value(imageHostS3Secret), PathPrefix: value(imageHostS3Prefix),
			PublicBaseURL: value(imageHostS3PublicBase), PathStyle: form.pathStyle,
		}
	}
	return saved, saved.Validate()
}

func (state *model) saveImageHost() {
	host := &state.imageHost
	form := host.form
	cfg, err := form.configuration(host.cfg)
	if err == nil {
		if host.store == nil {
			err = imagehost.ErrWriteStore
		} else {
			err = host.store.Save(cfg)
		}
	}
	if err != nil {
		form.feedback = state.imageHostErrorText(err)
	} else {
		host.cfg = cfg
		form.feedback = state.local("图片上传设置已保存", "Image upload settings saved")
	}
	state.invalidator.Invalidate()
}

func (state *model) testImageHost() {
	form := state.imageHost.form
	cfg, err := form.configuration(state.imageHost.cfg)
	if err != nil {
		form.feedback = state.imageHostErrorText(err)
		return
	}
	test := state.imageHost.test
	if test == nil {
		test = imagehost.TestConnection
	}
	form.feedback = state.local("正在上传测试图片…", "Uploading test image…")
	form.scan.start(state.invalidator.Invalidate, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, test(ctx, cfg)
	})
}

func (state *model) drainImageHost() {
	if form := state.imageHost.form; form != nil {
		if result, ready := form.scan.receive(); ready {
			form.feedback = state.local("测试上传成功", "Test upload succeeded")
			if result.err != nil {
				form.feedback = state.imageHostErrorText(result.err)
			}
		}
	}
	state.drainImagePaste()
}

func (state *model) imageHostErrorText(err error) string {
	var status imagehost.StatusError
	local := state.local
	switch {
	case errors.As(err, &status):
		return local("上传服务器拒绝请求：HTTP ", "Upload server rejected request: HTTP ") + strconv.Itoa(int(status))
	case errors.Is(err, imagehost.ErrTimeout):
		return local("上传超时，请检查网络", "Upload timed out; check your connection")
	case errors.Is(err, imagehost.ErrNetwork):
		return local("无法连接上传服务，请检查地址和网络", "Couldn't connect to the upload server; check the endpoint and network")
	case errors.Is(err, imagehost.ErrUnsafeURL), errors.Is(err, imagehost.ErrResponse):
		return local("上传服务未返回安全链接，请检查响应和 JSON 路径", "No safe URL returned; check the server response and JSON path")
	case errors.Is(err, imagehost.ErrConfig):
		return local("图片上传配置无效，请检查地址、参数和凭证", "Invalid image upload settings; check the endpoint, options and credentials")
	case errors.Is(err, errImageHostHeaders):
		return local("请求头必须为值全为字符串的 JSON 对象", "Headers must be a JSON object with string values")
	case errors.Is(err, errImageHostIME):
		return local("请先完成输入法组合", "Finish composing text first")
	case errors.Is(err, imagehost.ErrUnsafeStore):
		return local("图片上传配置路径或权限不安全", "Image upload settings path or permissions are unsafe")
	case errors.Is(err, imagehost.ErrCorruptStore):
		return local("图片上传配置无效或过大，未覆盖原文件", "Image upload settings are invalid or too large; original file kept")
	case errors.Is(err, imagehost.ErrReadStore), errors.Is(err, imagehost.ErrWriteStore):
		return local("无法读取或保存私有图片上传配置", "Couldn't read or save private image upload settings")
	default:
		return local("上传失败，请检查图片上传配置", "Upload failed; check image upload settings")
	}
}

func (view settingsView) imageHostRows() int {
	if form := view.state.imageHost.form; form != nil {
		switch form.mode {
		case imagehost.ModeHTTP:
			return 7
		case imagehost.ModeS3:
			return 10
		}
	}
	return 3
}

func (view settingsView) paintImageHost(bounds ui.Rect) {
	state := view.state
	form := state.imageHost.form
	if form == nil {
		return
	}
	local := state.local
	busy := form.scan.ready != nil
	rows := view.imageHostRows()
	mode := form.mode
	card := view.group(bounds, local("图片上传", "Image upload"), rows)
	view.segments(view.row(card, 0, local("保存方式", "Storage"), 312), string(mode), []settingOption{
		{imageHostLocalID, string(imagehost.ModeLocal), local("本地保存", "Local"), ""},
		{imageHostHTTPID, string(imagehost.ModeHTTP), local("自定义上传", "Custom HTTP"), ""},
		{imageHostS3ID, string(imagehost.ModeS3), local("对象存储 S3/OSS", "S3 / OSS"), ""},
	}, !busy, func(value string) {
		if form.mode != imagehost.Mode(value) {
			form.mode = imagehost.Mode(value)
			form.feedback = ""
			state.settings.scroll = 0
			state.invalidator.Invalidate()
		}
	})
	field := func(id gfx.ID, index int, label, placeholder string, secure bool) {
		area := view.row(card, index, label, min(float32(306), card.Width-200))
		input := form.fields[id].Editor
		focused := view.canvas.Focused() == id
		value := input.CommittedString()
		inputTheme := view.theme
		inputTheme.Metrics.FontSizeM = state.uiSize(12)
		inputTheme.Input = view.palette.muted.WithAlpha(.8)
		components.PaintInput(view.canvas, inputTheme, area, components.InputSnapshot{
			ID: id, Label: label, Placeholder: placeholder, Value: value, Secure: secure,
			Focused: focused, Disabled: busy, PlatformEditing: focused && value != "", ShowCaret: focused && value == "",
		})
	}
	switch mode {
	case imagehost.ModeHTTP:
		field(imageHostHTTPEndpoint, 1, local("上传地址", "Upload endpoint"), "https://", false)
		view.segments(view.row(card, 2, local("请求方法", "HTTP method"), 132), form.method, []settingOption{
			{imageHostModePostID, "POST", "POST", ""}, {imageHostModePutID, "PUT", "PUT", ""},
		}, !busy, func(value string) { form.method = value })
		field(imageHostHTTPFileField, 3, local("文件字段", "File field"), "file", false)
		field(imageHostHTTPJSONPath, 4, local("响应 JSON 路径", "Response JSON path"), "data.url", false)
		field(imageHostHTTPHeaders, 5, local("请求头（隐藏）", "Headers (hidden)"), `{"Header":"value"}`, true)
	case imagehost.ModeS3:
		field(imageHostS3Endpoint, 1, local("上传地址", "Upload endpoint"), "https://", false)
		field(imageHostS3Region, 2, local("区域", "Region"), "us-east-1", false)
		field(imageHostS3Bucket, 3, local("存储桶", "Bucket"), "", false)
		field(imageHostS3AccessKey, 4, "Access Key ID", "", true)
		field(imageHostS3Secret, 5, "Secret Access Key", "", true)
		field(imageHostS3Prefix, 6, local("对象前缀", "Object prefix"), "images/", false)
		field(imageHostS3PublicBase, 7, local("公开 URL 前缀", "Public URL base"), "https://", false)
		label := local("使用路径式地址", "Use path-style URLs")
		if view.switchControl(imageHostPathStyleID, view.row(card, 8, label, 44), label, form.pathStyle, !busy) {
			form.pathStyle = !form.pathStyle
		}
	default:
		view.value(view.row(card, 1, local("本地目录", "Local directory"), 96), "assets/")
	}
	save, test := local("保存", "Save"), local("测试上传", "Test Upload")
	actions := view.row(card, rows-1, "", 214)
	view.button(imageHostTestID, test, ui.Rect{X: actions.X, Y: actions.Y, Width: 122, Height: actions.Height}, false, !busy && mode != imagehost.ModeLocal, state.testImageHost)
	actions.X, actions.Width = actions.X+130, 84
	view.button(imageHostSaveID, save, actions, true, !busy, state.saveImageHost)
}
