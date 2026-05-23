package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/database"
	"github.com/sjzar/chatlog/internal/errors"
)

const DefaultFunASRPort = 45987

type Service struct {
	conf Config
	db   *database.Service

	router *gin.Engine
	server *http.Server

	mcpServer           *server.MCPServer
	mcpSSEServer        *server.SSEServer
	mcpStreamableServer *server.StreamableHTTPServer

	// md5 到 path 的缓存（用于图片、视频等媒体文件）
	md5PathCache map[string]string
	md5PathMu    sync.RWMutex

	// FunASR 守护进程
	funASRProcess   *os.Process
	funASRPort      int
	funASRProcessMu sync.Mutex
}

type Config interface {
	GetHTTPAddr() string
	GetDataDir() string
	GetSaveDecryptedMedia() bool
	GetWorkDir() string
	GetPlatform() string
	GetVersion() int
	GetDataKey() string
	GetImgKey() string
	GetAutoDecrypt() bool
	GetWalEnabled() bool
	GetAutoDecryptDebounce() int
	GetWebhook() *conf.Webhook
	GetFunASRPython() string
	GetFunASRScript() string
	GetFunASRTimeoutSec() int
}

// FunASRRuntime 定义了 FunASR 运行时所需的配置接口
type FunASRRuntime interface {
	GetFunASRPython() string
	GetFunASRScript() string
	GetFunASRTimeoutSec() int
}

func NewService(conf Config, db *database.Service) *Service {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Handle error from SetTrustedProxies
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Err(err).Msg("Failed to set trusted proxies")
	}

	// Middleware
	router.Use(
		errors.RecoveryMiddleware(),
		errors.ErrorHandlerMiddleware(),
		gin.LoggerWithWriter(log.Logger, "/health"),
		corsMiddleware(),
	)

	s := &Service{
		conf:         conf,
		db:           db,
		router:       router,
		md5PathCache: make(map[string]string),
	}

	s.initMCPServer()
	s.initRouter()
	return s
}

func (s *Service) Start() error {

	s.server = &http.Server{
		Addr:    s.conf.GetHTTPAddr(),
		Handler: s.router,
	}

	// 启动 FunASR 守护进程
	go func() {
		if _, err := s.ensureFunASRDaemon(); err != nil {
			log.Warn().Err(err).Msg("Failed to start FunASR daemon, will start on first use")
		} else {
			log.Info().Msg("FunASR daemon started during service startup")
		}
	}()

	go func() {
		// Handle error from Run
		if err := s.server.ListenAndServe(); err != nil {
			log.Err(err).Msg("Failed to start HTTP server")
		}
	}()

	log.Info().Msg("Starting HTTP server on " + s.conf.GetHTTPAddr())

	return nil
}

func (s *Service) ListenAndServe() error {

	s.server = &http.Server{
		Addr:    s.conf.GetHTTPAddr(),
		Handler: s.router,
	}

	// 启动 FunASR 守护进程
	if _, err := s.ensureFunASRDaemon(); err != nil {
		log.Warn().Err(err).Msg("Failed to start FunASR daemon, will start on first use")
	} else {
		log.Info().Msg("FunASR daemon started during service startup")
	}

	log.Info().Msg("Starting HTTP server on " + s.conf.GetHTTPAddr())
	return s.server.ListenAndServe()
}

func (s *Service) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		log.Debug().Err(err).Msg("Failed to shutdown HTTP server")
		return nil
	}

	s.stopFunASRDaemon()
	log.Info().Msg("HTTP server stopped")
	return nil
}

func (s *Service) stopFunASRDaemon() {
	s.funASRProcessMu.Lock()
	defer s.funASRProcessMu.Unlock()
	if s.funASRProcess != nil {
		s.funASRProcess.Kill()
		s.funASRProcess = nil
	}
}

func (s *Service) ensureFunASRDaemon() (int, error) {
	s.funASRProcessMu.Lock()
	defer s.funASRProcessMu.Unlock()

	if s.funASRProcess != nil {
		return s.funASRPort, nil
	}

	pythonPath := strings.TrimSpace(s.conf.GetFunASRPython())
	if pythonPath == "" {
		return 0, fmt.Errorf("未配置 funasr.python")
	}

	script, err := s.resolveFunASRScript()
	if err != nil {
		return 0, err
	}

	port := s.funASRPort
	if port == 0 {
		port = DefaultFunASRPort
	}

	cmd := exec.Command(pythonPath, script, "--daemon", fmt.Sprintf("%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = filepath.Dir(script)

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动 FunASR 守护进程失败: %w", err)
	}

	s.funASRProcess = cmd.Process
	s.funASRPort = port

	time.Sleep(2 * time.Second)
	if s.funASRProcess == nil || !processExists(s.funASRProcess.Pid) {
		return 0, fmt.Errorf("FunASR 守护进程启动失败")
	}

	log.Info().Int("port", port).Msg("FunASR daemon started")
	return port, nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func (s *Service) transcribeViaSocket(port int, audioPath string) (string, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("连接 FunASR 守护进程失败: %w", err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return "", err
	}
	if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return "", err
	}

	if _, err := conn.Write([]byte(audioPath)); err != nil {
		return "", fmt.Errorf("发送音频路径失败: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("读取转写结果失败: %w", err)
	}

	result := string(buf[:n])
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		return "", fmt.Errorf("解析转写结果失败: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("转写错误: %s", out.Error)
	}
	return out.Text, nil
}

func (s *Service) resolveFunASRScript() (string, error) {
	if p := strings.TrimSpace(s.conf.GetFunASRScript()); p != "" {
		return p, nil
	}
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "scripts", "funasr_transcribe.py")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("解析 funasr 脚本路径失败: %w", err)
	}
	for _, rel := range [][]string{{"scripts", "funasr_transcribe.py"}, {"..", "scripts", "funasr_transcribe.py"}} {
		cand := filepath.Join(filepath.Dir(exe), filepath.Join(rel...))
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("未找到 scripts/funasr_transcribe.py")
}

func (s *Service) GetRouter() *gin.Engine {
	return s.router
}
