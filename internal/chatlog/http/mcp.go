package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/errors"
	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/pkg/util"
	"github.com/sjzar/chatlog/pkg/util/dat2img"
	"github.com/sjzar/chatlog/pkg/util/silk"
	"github.com/sjzar/chatlog/pkg/version"
)

func (s *Service) initMCPServer() {
	s.mcpServer = server.NewMCPServer(conf.AppName, version.Version,
		server.WithResourceCapabilities(false, false),
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)
	s.mcpServer.AddTool(ContactTool, s.handleMCPContact)
	s.mcpServer.AddTool(ChatRoomTool, s.handleMCPChatRoom)
	s.mcpServer.AddTool(RecentChatTool, s.handleMCPRecentChat)
	s.mcpServer.AddTool(ChatLogTool, s.handleMCPChatLog)
	s.mcpServer.AddTool(CurrentTimeTool, s.handleMCPCurrentTime)
	s.mcpServer.AddTool(GetMediaContentTool, s.handleMCPGetMediaContent)
	s.mcpServer.AddTool(OCRImageMessageTool, s.handleMCPOCRImageMessage)
	s.mcpServer.AddTool(SendWebhookNotificationTool, s.handleMCPSendWebhookNotification)
	s.mcpServer.AddTool(AnalyzeChatActivityTool, s.handleMCPAnalyzeChatActivity)
	s.mcpServer.AddTool(GetUserProfileTool, s.handleMCPGetUserProfile)
	s.mcpServer.AddTool(SearchSharedFilesTool, s.handleMCPSearchSharedFiles)
	s.mcpServer.AddPrompt(ChatSummaryDailyPrompt, s.handleMCPChatSummaryDaily)
	s.mcpServer.AddPrompt(ConflictDetectorPrompt, s.handleMCPConflictDetector)
	s.mcpServer.AddPrompt(RelationshipMilestonesPrompt, s.handleMCPRelationshipMilestones)
	s.mcpSSEServer = server.NewSSEServer(s.mcpServer,
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
	)
	s.mcpStreamableServer = server.NewStreamableHTTPServer(s.mcpServer)
}

var ChatSummaryDailyPrompt = mcp.NewPrompt(
	"chat_summary_daily",
	mcp.WithPromptDescription("生成每日聊天摘要模板。"),
	mcp.WithArgument("date", mcp.ArgumentDescription("摘要日期 (YYYY-MM-DD)"), mcp.RequiredArgument()),
	mcp.WithArgument("talker", mcp.ArgumentDescription("对话方 ID"), mcp.RequiredArgument()),
)

var ConflictDetectorPrompt = mcp.NewPrompt(
	"conflict_detector",
	mcp.WithPromptDescription("情绪与冲突检测模板。"),
	mcp.WithArgument("talker", mcp.ArgumentDescription("对话方 ID"), mcp.RequiredArgument()),
)

var RelationshipMilestonesPrompt = mcp.NewPrompt(
	"relationship_milestones",
	mcp.WithPromptDescription("关系里程碑回顾模板。"),
	mcp.WithArgument("talker", mcp.ArgumentDescription("对话方 ID"), mcp.RequiredArgument()),
)

var SearchSharedFilesTool = mcp.NewTool(
	"search_shared_files",
	mcp.WithDescription(`专门搜索聊天记录中发送的文件元数据。当用户想找某个特定的共享文件时使用。`),
	mcp.WithString("talker", mcp.Description("对话方 ID"), mcp.Required()),
	mcp.WithString("keyword", mcp.Description("文件名搜索关键词")),
)

var AnalyzeChatActivityTool = mcp.NewTool(
	"analyze_chat_activity",
	mcp.WithDescription(`统计特定时间段内对话方的活跃度，包括发言频率、活跃时段等。用于分析某人的社交习惯或群聊热度。`),
	mcp.WithString("time", mcp.Description("时间范围 (例如: 2023-04-01~2023-04-18)"), mcp.Required()),
	mcp.WithString("talker", mcp.Description("对话方 ID"), mcp.Required()),
)

var GetUserProfileTool = mcp.NewTool(
	"get_user_profile",
	mcp.WithDescription(`获取联系人或群组的详细资料，包括备注、属性、群成员（如果是群组）等背景信息。用于更深入地了解对话方。`),
	mcp.WithString("key", mcp.Description("联系人或群组的 ID 或名称"), mcp.Required()),
)

var SendWebhookNotificationTool = mcp.NewTool(
	"send_webhook_notification",
	mcp.WithDescription(`触发外部 Webhook 通知。当模型完成聊天记录分析、发现重要事项或需要提醒外部系统时使用此工具。`),
	mcp.WithString("url", mcp.Description("Webhook 接收地址"), mcp.Required()),
	mcp.WithString("message", mcp.Description("要发送的通知内容或分析结果"), mcp.Required()),
	mcp.WithString("level", mcp.Description("通知级别 (info, warn, error)")),
)

var OCRImageMessageTool = mcp.NewTool(
	"ocr_image_message",
	mcp.WithDescription(`对特定图片消息进行 OCR 解析以提取其中的文字。`),
	mcp.WithString("talker", mcp.Description("消息所在的对话方（联系人 ID 或群 ID）"), mcp.Required()),
	mcp.WithNumber("message_id", mcp.Description("消息的唯一 ID (Seq)"), mcp.Required()),
)

var GetMediaContentTool = mcp.NewTool(
	"get_media_content",
	mcp.WithDescription(`根据消息 ID 获取解码后的媒体文件内容（图片或语音）。当聊天记录中显示 [图片] 或 [语音] 且用户需要查看具体内容或进行分析时使用此工具。`),
	mcp.WithString("talker", mcp.Description("消息所在的对话方（联系人 ID 或群 ID）"), mcp.Required()),
	mcp.WithNumber("message_id", mcp.Description("消息的唯一 ID (Seq)"), mcp.Required()),
)

var ContactTool = mcp.NewTool(
	"query_contact",
	mcp.WithDescription(`查询用户的联系人信息。可以通过姓名、备注名或ID进行查询，返回匹配的联系人列表。当用户询问某人的联系方式、想了解联系人信息或需要查找特定联系人时使用此工具。参数为空时，将返回联系人列表`),
	mcp.WithString("keyword", mcp.Description("联系人的搜索关键词，可以是姓名、备注名或ID。")),
)

var ChatRoomTool = mcp.NewTool(
	"query_chat_room",
	mcp.WithDescription(`查询用户参与的群聊信息。可以通过群名称、群ID或相关关键词进行查询，返回匹配的群聊列表。当用户询问群聊信息、想了解某个群的详情或需要查找特定群聊时使用此工具。`),
	mcp.WithString("keyword", mcp.Description("群聊的搜索关键词，可以是群名称、群ID或相关描述")),
)

var RecentChatTool = mcp.NewTool(
	"query_recent_chat",
	mcp.WithDescription(`查询最近会话列表，包括个人聊天和群聊。当用户想了解最近的聊天记录、查看最近联系过的人或群组时使用此工具。不需要参数，直接返回最近的会话列表。`),
)

var ChatLogTool = mcp.NewTool(
	"query_chat_log",
	mcp.WithDescription(chatLogToolDescription),
	mcp.WithString("time", mcp.Description(chatLogTimeParamDescription), mcp.Required()),
	mcp.WithString("talker", mcp.Description(`指定对话方（联系人或群组）
- 可使用ID、昵称或备注名
- 多个对话方用","分隔，如："张三,李四,工作群"
- 【重要】这是多步查询中唯一应保留的参数`), mcp.Required()),
	mcp.WithString("sender", mcp.Description(chatLogSenderParamDescription)),
	mcp.WithString("keyword", mcp.Description(chatLogKeywordParamDescription)),
)

var CurrentTimeTool = mcp.NewTool(
	"current_time",
	mcp.WithDescription(currentTimeToolDescription),
)

type ContactRequest struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (s *Service) handleMCPContact(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req ContactRequest
	if err := request.BindArguments(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind arguments")
		log.Error().Interface("request", request.GetRawArguments()).Msg("Failed to bind arguments")
		return errors.ErrMCPTool(err), nil
	}

	list, err := s.db.GetContacts(req.Keyword, req.Limit, req.Offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get contacts")
		return errors.ErrMCPTool(err), nil
	}
	buf := &bytes.Buffer{}
	buf.WriteString("UserName,Alias,Remark,NickName\n")
	for _, contact := range list.Items {
		buf.WriteString(fmt.Sprintf("%s,%s,%s,%s\n", contact.UserName, contact.Alias, contact.Remark, contact.NickName))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

type ChatRoomRequest struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (s *Service) handleMCPChatRoom(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	var req ChatRoomRequest
	if err := request.BindArguments(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind arguments")
		log.Error().Interface("request", request.GetRawArguments()).Msg("Failed to bind arguments")
		return errors.ErrMCPTool(err), nil
	}

	list, err := s.db.GetChatRooms(req.Keyword, req.Limit, req.Offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get chat rooms")
		return errors.ErrMCPTool(err), nil
	}
	buf := &bytes.Buffer{}
	buf.WriteString("Name,Remark,NickName,Owner,UserCount\n")
	for _, chatRoom := range list.Items {
		buf.WriteString(fmt.Sprintf("%s,%s,%s,%s,%d\n", chatRoom.Name, chatRoom.Remark, chatRoom.NickName, chatRoom.Owner, len(chatRoom.Users)))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

type RecentChatRequest struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (s *Service) handleMCPRecentChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	var req RecentChatRequest
	if err := request.BindArguments(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind arguments")
		log.Error().Interface("request", request.GetRawArguments()).Msg("Failed to bind arguments")
		return errors.ErrMCPTool(err), nil
	}

	data, err := s.db.GetSessions(req.Keyword, req.Limit, req.Offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get sessions")
		return errors.ErrMCPTool(err), nil
	}
	buf := &bytes.Buffer{}
	for _, session := range data.Items {
		buf.WriteString(session.PlainText(120))
		buf.WriteString("\n")
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

type ChatLogRequest struct {
	Time    string `form:"time"`
	Talker  string `form:"talker"`
	Sender  string `form:"sender"`
	Keyword string `form:"keyword"`
	Limit   int    `form:"limit"`
	Offset  int    `form:"offset"`
	Format  string `form:"format"`
}

func (s *Service) handleMCPChatLog(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

	var req ChatLogRequest
	if err := request.BindArguments(&req); err != nil {
		log.Error().Err(err).Msg("Failed to bind arguments")
		log.Error().Interface("request", request.GetRawArguments()).Msg("Failed to bind arguments")
		return errors.ErrMCPTool(err), nil
	}

	var err error
	start, end, ok := util.TimeRangeOf(req.Time)
	if !ok {
		log.Error().Str("time", req.Time).Msg("Failed to parse time range")
		return errors.ErrMCPTool(fmt.Errorf("invalid time format: %s", req.Time)), nil
	}
	if req.Limit < 0 {
		req.Limit = 0
	}

	if req.Offset < 0 {
		req.Offset = 0
	}

	messages, err := s.db.GetMessages(start, end, req.Talker, req.Sender, req.Keyword, req.Limit, req.Offset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get messages")
		return errors.ErrMCPTool(err), nil
	}

	buf := &bytes.Buffer{}
	if len(messages) == 0 {
		buf.WriteString("未找到符合查询条件的聊天记录")
	}
	for _, m := range messages {
		buf.WriteString(m.PlainText(strings.Contains(req.Talker, ","), util.PerfectTimeFormat(start, end), ""))
		buf.WriteString("\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

func (s *Service) handleMCPCurrentTime(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: time.Now().Local().Format(time.RFC3339),
			},
		},
	}, nil
}

type GetMediaContentRequest struct {
	Talker    string `json:"talker"`
	MessageID int64  `json:"message_id"`
}

func (s *Service) handleMCPGetMediaContent(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req GetMediaContentRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	msg, err := s.db.GetMessage(req.Talker, req.MessageID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get message")
		return errors.ErrMCPTool(err), nil
	}

	switch msg.Type {
	case model.MessageTypeImage:
		return s.handleMCPGetImage(ctx, msg)
	case model.MessageTypeVoice:
		return s.handleMCPGetVoice(ctx, msg)
	default:
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("暂不支持的消息类型: %d", msg.Type),
				},
			},
		}, nil
	}
}

func (s *Service) handleMCPOCRImageMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req GetMediaContentRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	msg, err := s.db.GetMessage(req.Talker, req.MessageID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get message")
		return errors.ErrMCPTool(err), nil
	}

	if msg.Type != model.MessageTypeImage {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "该消息不是图片消息，无法进行 OCR 解析。",
				},
			},
		}, nil
	}

	result, err := s.handleMCPGetImage(ctx, msg)
	if err != nil {
		return result, err
	}

	// 在结果中添加一条提示信息
	result.Content = append([]mcp.Content{
		mcp.TextContent{
			Type: "text",
			Text: "已提取图片数据，请直接分析该图片内容并提取文字 (OCR)。",
		},
	}, result.Content...)

	return result, nil
}

func (s *Service) handleMCPGetImage(ctx context.Context, msg *model.Message) (*mcp.CallToolResult, error) {
	key, ok := msg.Contents["md5"].(string)
	if !ok {
		// 尝试从 path 获取
		key, _ = msg.Contents["path"].(string)
	}

	if key == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "无法找到图片标识符",
				},
			},
		}, nil
	}

	media, err := s.db.GetMedia("image", key)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}

	absolutePath := filepath.Join(s.conf.GetDataDir(), media.Path)
	b, err := os.ReadFile(absolutePath)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}

	var data []byte
	var mimeType string

	if strings.HasSuffix(strings.ToLower(media.Path), ".dat") {
		out, ext, err := dat2img.Dat2Image(b)
		if err != nil {
			return errors.ErrMCPTool(err), nil
		}
		data = out
		switch ext {
		case "png":
			mimeType = "image/png"
		case "gif":
			mimeType = "image/gif"
		case "bmp":
			mimeType = "image/bmp"
		default:
			mimeType = "image/jpeg"
		}
	} else {
		data = b
		ext := strings.ToLower(filepath.Ext(media.Path))
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".bmp":
			mimeType = "image/bmp"
		default:
			mimeType = "image/jpeg"
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.ImageContent{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(data),
				MIMEType: mimeType,
			},
		},
	}, nil
}

func (s *Service) handleMCPGetVoice(ctx context.Context, msg *model.Message) (*mcp.CallToolResult, error) {
	out, err := s.getVoiceMP3Bytes(msg)
	if err != nil {
		switch {
		case err.Error() == "无法找到语音标识符":
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: "text", Text: err.Error()}},
			}, nil
		case strings.Contains(err.Error(), "语音转换失败"):
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: "text", Text: err.Error()}},
			}, nil
		default:
			return errors.ErrMCPTool(err), nil
		}
	}

	// 创建一个实现了FunASRRuntime接口的结构体
	fc := &funASRConfigWrapper{conf: s.conf}
	
	// 检查Python路径是否配置
	if strings.TrimSpace(s.conf.GetFunASRPython()) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "无法转写语音：未在服务配置中设置 funasr.python。请配置 Python 可执行路径、安装 FunASR（pip install funasr modelscope torch torchaudio）并保证 scripts/funasr_transcribe.py 可用。",
				},
			},
		}, nil
	}

	return s.funASRTranscribeMP3(ctx, fc, out), nil
}

// funASRConfigWrapper 包装Config接口，实现FunASRRuntime接口
type funASRConfigWrapper struct {
	conf Config
}

func (w *funASRConfigWrapper) GetFunASRPython() string {
	return w.conf.GetFunASRPython()
}

func (w *funASRConfigWrapper) GetFunASRScript() string {
	return w.conf.GetFunASRScript()
}

func (w *funASRConfigWrapper) GetFunASRTimeoutSec() int {
	return w.conf.GetFunASRTimeoutSec()
}

// getVoiceMP3Bytes 将微信语音消息解码为 MP3 字节（Silk → MP3）。
func (s *Service) getVoiceMP3Bytes(msg *model.Message) ([]byte, error) {
	key, ok := msg.Contents["voice"].(string)
	if !ok {
		return nil, fmt.Errorf("无法找到语音标识符")
	}
	media, err := s.db.GetMedia("voice", key)
	if err != nil {
		return nil, err
	}
	out, err := silk.Silk2MP3(media.Data)
	if err != nil {
		return nil, fmt.Errorf("语音转换失败: %w; 原始语音数据(base64): %s", err, base64.StdEncoding.EncodeToString(media.Data))
	}
	return out, nil
}

type SendWebhookNotificationRequest struct {
	URL     string `json:"url"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

func (s *Service) handleMCPSendWebhookNotification(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req SendWebhookNotificationRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	payload := map[string]interface{}{
		"message":   req.Message,
		"level":     req.Level,
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "chatlog-mcp",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", req.URL, bytes.NewBuffer(body))
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.ErrMCPTool(fmt.Errorf("webhook returned status %d", resp.StatusCode)), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: "Webhook 通知发送成功。",
			},
		},
	}, nil
}

type AnalyzeChatActivityRequest struct {
	Time   string `json:"time"`
	Talker string `json:"talker"`
}

func (s *Service) handleMCPAnalyzeChatActivity(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req AnalyzeChatActivityRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	start, end, ok := util.TimeRangeOf(req.Time)
	if !ok {
		return errors.ErrMCPTool(fmt.Errorf("invalid time format: %s", req.Time)), nil
	}

	messages, err := s.db.GetMessages(start, end, req.Talker, "", "", 0, 0)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}

	if len(messages) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "该时间段内没有聊天记录。",
				},
			},
		}, nil
	}

	// 统计逻辑
	totalCount := len(messages)
	senderStats := make(map[string]int)
	hourStats := make(map[int]int)
	typeStats := make(map[int64]int)

	for _, m := range messages {
		sender := m.SenderName
		if sender == "" {
			sender = m.Sender
		}
		senderStats[sender]++
		hourStats[m.Time.Hour()]++
		typeStats[m.Type]++
	}

	buf := &bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("分析报告 (%s - %s)\n", start.Format(time.DateOnly), end.Format(time.DateOnly)))
	buf.WriteString(fmt.Sprintf("总消息数: %d\n\n", totalCount))

	buf.WriteString("发言频率排行:\n")
	type senderStat struct {
		Name  string
		Count int
	}
	ss := make([]senderStat, 0, len(senderStats))
	for name, count := range senderStats {
		ss = append(ss, senderStat{name, count})
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Count > ss[j].Count })
	for i, s := range ss {
		if i >= 10 {
			break
		} // 只显示前 10
		percentage := float64(s.Count) / float64(totalCount) * 100
		buf.WriteString(fmt.Sprintf("- %s: %d (%.1f%%)\n", s.Name, s.Count, percentage))
	}

	buf.WriteString("\n活跃时段分布:\n")
	for h := 0; h < 24; h++ {
		if count, ok := hourStats[h]; ok {
			buf.WriteString(fmt.Sprintf("%02d:00: %s (%d)\n", h, strings.Repeat("█", (count*20+totalCount-1)/totalCount), count))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

type GetUserProfileRequest struct {
	Key string `json:"key"`
}

func (s *Service) handleMCPGetUserProfile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req GetUserProfileRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	buf := &bytes.Buffer{}

	// 尝试作为群聊获取
	if chatRoom, err := s.db.GetChatRoom(req.Key); err == nil {
		buf.WriteString(fmt.Sprintf("【群聊资料】\n"))
		buf.WriteString(fmt.Sprintf("ID: %s\n", chatRoom.Name))
		buf.WriteString(fmt.Sprintf("名称: %s\n", chatRoom.NickName))
		if chatRoom.Remark != "" {
			buf.WriteString(fmt.Sprintf("备注: %s\n", chatRoom.Remark))
		}
		buf.WriteString(fmt.Sprintf("群主: %s\n", chatRoom.Owner))
		buf.WriteString(fmt.Sprintf("成员数: %d\n", len(chatRoom.Users)))
		buf.WriteString("\n部分成员列表:\n")
		for i, user := range chatRoom.Users {
			if i >= 20 {
				buf.WriteString("... 等等\n")
				break
			}
			displayName := chatRoom.User2DisplayName[user.UserName]
			buf.WriteString(fmt.Sprintf("- %s (%s)\n", displayName, user.UserName))
		}
	} else if contact, err := s.db.GetContact(req.Key); err == nil {
		// 尝试作为联系人获取
		buf.WriteString(fmt.Sprintf("【联系人资料】\n"))
		buf.WriteString(fmt.Sprintf("ID: %s\n", contact.UserName))
		buf.WriteString(fmt.Sprintf("昵称: %s\n", contact.NickName))
		if contact.Remark != "" {
			buf.WriteString(fmt.Sprintf("备注: %s\n", contact.Remark))
		}
		if contact.Alias != "" {
			buf.WriteString(fmt.Sprintf("微信号: %s\n", contact.Alias))
		}
		buf.WriteString(fmt.Sprintf("是否好友: %v\n", contact.IsFriend))
	} else {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("未找到相关联系人或群组: %s", req.Key),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: buf.String(),
			},
		},
	}, nil
}

type SearchSharedFilesRequest struct {
	Talker  string `json:"talker"`
	Keyword string `json:"keyword"`
}

func (s *Service) handleMCPSearchSharedFiles(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var req SearchSharedFilesRequest
	if err := request.BindArguments(&req); err != nil {
		return errors.ErrMCPTool(err), nil
	}

	// 查找 MessageTypeShare (49) 且 MessageSubTypeFile (6)
	messages, err := s.db.GetMessages(time.Time{}, time.Now(), req.Talker, "", req.Keyword, 50, 0)
	if err != nil {
		return errors.ErrMCPTool(err), nil
	}

	buf := &bytes.Buffer{}
	count := 0
	for _, m := range messages {
		if m.Type == model.MessageTypeShare && m.SubType == model.MessageSubTypeFile {
			title, _ := m.Contents["title"].(string)
			buf.WriteString(fmt.Sprintf("[%d] %s - %s\n", m.Seq, m.Time.Format("2006-01-02 15:04"), title))
			count++
		}
	}

	if count == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: "未找到相关共享文件。",
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("找到 %d 个文件:\n%s", count, buf.String()),
			},
		},
	}, nil
}

func (s *Service) handleMCPChatSummaryDaily(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	date := request.Params.Arguments["date"]
	talker := request.Params.Arguments["talker"]

	return mcp.NewGetPromptResult(
		"每日聊天摘要指令",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("请分析并在总结 %s 在 %s 的聊天内容。请先使用 query_chat_log 获取当天的完整记录，然后从关键话题、重要决策、待办事项三个维度进行总结。", talker, date),
			}),
		},
	), nil
}

func (s *Service) handleMCPConflictDetector(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	talker := request.Params.Arguments["talker"]

	return mcp.NewGetPromptResult(
		"情绪与冲突检测指令",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("请分析与 %s 最近的聊天记录，识别是否存在潜在的情绪波动或冲突。请关注语气变化、负面词汇频率以及争议性话题。", talker),
			}),
		},
	), nil
}

func (s *Service) handleMCPRelationshipMilestones(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	talker := request.Params.Arguments["talker"]

	return mcp.NewGetPromptResult(
		"关系里程碑回顾指令",
		[]mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("请回顾与 %s 的历史聊天记录，找出重要的关系里程碑（如：初次相识、重大合作达成、共同解决的危机等）。", talker),
			}),
		},
	), nil
}


