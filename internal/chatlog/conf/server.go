package conf

import "strings"

const (
	DefalutHTTPAddr = "0.0.0.0:5030"
	DefaultPython   = "E:\\miniconda3\\envs\\funasr\\python.exe"
)

// GetFunASRPython 获取 FunASR Python 可执行路径
func GetFunASRPython(config *FunASRConfig) string {
	if config == nil {
		return DefaultPython
	}
	if p := strings.TrimSpace(config.Python); p != "" {
		return p
	}
	return DefaultPython
}

// GetFunASRScript 获取 FunASR 脚本路径
func GetFunASRScript(config *FunASRConfig) string {
	if config == nil {
		return ""
	}
	return strings.TrimSpace(config.Script)
}

// GetFunASRTimeoutSec 获取 FunASR 超时时间
func GetFunASRTimeoutSec(config *FunASRConfig) int {
	if config == nil || config.TimeoutSec <= 0 {
		return 900
	}
	return config.TimeoutSec
}

// FunASRConfig 启用 get_media_content 对 [语音] 的转文字（本地 Python + modelscope/FunASR）。
// 脚本所需的所有参数（device、model_dir）都由脚本通过环境变量决定。
// 文档：https://github.com/modelscope/FunASR
type FunASRConfig struct {
	Python     string `mapstructure:"python"`      // 必填，Python 可执行文件路径
	Script     string `mapstructure:"script"`      // 可选，默认同目录下 scripts/funasr_transcribe.py
	TimeoutSec int    `mapstructure:"timeout_sec"` // 可选，默认 900（首次会下载模型）
}

type ServerConfig struct {
	Type               string   `mapstructure:"type"`
	Platform           string   `mapstructure:"platform"`
	Version            int      `mapstructure:"version"`
	FullVersion        string   `mapstructure:"full_version"`
	DataDir            string   `mapstructure:"data_dir"`
	DataKey            string   `mapstructure:"data_key"`
	ImgKey             string   `mapstructure:"img_key"`
	WorkDir            string   `mapstructure:"work_dir"`
	HTTPAddr           string   `mapstructure:"http_addr"`
	AutoDecrypt        bool     `mapstructure:"auto_decrypt"`
	WalEnabled         bool     `mapstructure:"wal_enabled"`
	AutoDecryptDebounce int     `mapstructure:"auto_decrypt_debounce"`
	SaveDecryptedMedia bool     `mapstructure:"save_decrypted_media"`
	Webhook            *Webhook `mapstructure:"webhook"`
	FunASR             *FunASRConfig `mapstructure:"funasr"`
}

var ServerDefaults = map[string]any{
	"save_decrypted_media": true,
}

func (c *ServerConfig) GetDataDir() string {
	return c.DataDir
}

func (c *ServerConfig) GetWorkDir() string {
	return c.WorkDir
}

func (c *ServerConfig) GetPlatform() string {
	return c.Platform
}

func (c *ServerConfig) GetVersion() int {
	return c.Version
}

func (c *ServerConfig) GetDataKey() string {
	return c.DataKey
}

func (c *ServerConfig) GetImgKey() string {
	return c.ImgKey
}

func (c *ServerConfig) GetAutoDecrypt() bool {
	return c.AutoDecrypt
}

func (c *ServerConfig) GetWalEnabled() bool {
	return c.WalEnabled
}

func (c *ServerConfig) GetAutoDecryptDebounce() int {
	return c.AutoDecryptDebounce
}

func (c *ServerConfig) GetHTTPAddr() string {
	if c.HTTPAddr == "" {
		c.HTTPAddr = DefalutHTTPAddr
	}
	return c.HTTPAddr
}

func (c *ServerConfig) GetWebhook() *Webhook {
	return c.Webhook
}

func (c *ServerConfig) GetSaveDecryptedMedia() bool {
	return c.SaveDecryptedMedia
}

func (c *ServerConfig) GetFunASRPython() string {
	return GetFunASRPython(c.FunASR)
}

func (c *ServerConfig) GetFunASRScript() string {
	return GetFunASRScript(c.FunASR)
}

func (c *ServerConfig) GetFunASRTimeoutSec() int {
	return GetFunASRTimeoutSec(c.FunASR)
}
