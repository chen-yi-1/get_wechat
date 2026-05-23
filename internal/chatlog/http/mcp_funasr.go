package http

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog/log"
)

func (s *Service) funASRTranscribeMP3(ctx context.Context, fc FunASRRuntime, mp3 []byte) *mcp.CallToolResult {
	port, err := s.ensureFunASRDaemon()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.TextContent{Type: "text", Text: fmt.Sprintf("启动 FunASR 守护进程失败: %v", err)}},
		}
	}

	tmp, err := os.CreateTemp("", "chatlog-voice-*.mp3")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.TextContent{Type: "text", Text: fmt.Sprintf("创建临时文件失败: %v", err)}},
		}
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, mp3, 0o600); err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.TextContent{Type: "text", Text: fmt.Sprintf("写入临时音频失败: %v", err)}},
		}
	}

	text, err := s.transcribeViaSocket(port, tmpPath)
	if err != nil {
		log.Warn().Err(err).Msg("funasr transcribe via socket failed")
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.TextContent{Type: "text", Text: fmt.Sprintf("转写失败: %v", err)}},
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: strings.TrimSpace(text)}},
	}
}