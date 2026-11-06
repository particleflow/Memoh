package tools

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/albumwriteback"
)

// AlbumWritebackProvider queues finished media for the user's phone album.
// The tool speaks in the user's product terms only: a pairing label, the
// content hashes, and what the queue now holds. Nothing about the delivery
// mechanism is model-facing.
type AlbumWritebackProvider struct {
	service *albumwriteback.Service
	logger  *slog.Logger
}

func NewAlbumWritebackProvider(log *slog.Logger, service *albumwriteback.Service) *AlbumWritebackProvider {
	if log == nil {
		log = slog.Default()
	}
	return &AlbumWritebackProvider{service: service, logger: log.With(slog.String("tool", "album_writeback"))}
}

// Usage describes when queueing is the right action. It is injected only when
// the provider registers its tool for the session.
func (p *AlbumWritebackProvider) Usage(_ context.Context, _ SessionContext, available AvailableTools) string {
	if p == nil || p.service == nil {
		return ""
	}
	ref, ok := available.Ref(ToolQueueAlbumWriteback())
	if !ok {
		return ""
	}
	return usageSection("Album write-back", []string{
		"After a processing step produced media the user asked to keep in a phone album, call " + ref + " with the pairing label the user designated and the content hashes of the finished items.",
		"Queueing is the handoff: it does not wait for, retry or report the phone write, and a queued item is written the next time the user opens the app.",
	})
}

var errAlbumWritebackUnavailable = errors.New("album write-back is not configured on this server")

func (p *AlbumWritebackProvider) Tools(_ context.Context, session SessionContext) ([]toolexec.Tool, error) {
	if p.service == nil {
		return nil, nil
	}
	sess := session
	return []toolexec.Tool{
		{
			Name:        ToolQueueAlbumWriteback().String(),
			Description: "把处理完成的媒体排入用户的手机相册写回队列。参数：pair_key 是用户指定相册时使用的配对标签；content_hashes 是本 Bot 已完成媒体的 content_hash 列表（取自处理结果或上传记录）；name 可选，给这一批文件起的显示名。返回 pair_key 与本次确认排入的 content_hash。排入只代表已登记：手机在用户下次打开应用时写入相册，写入结果不回传本次调用。同一配对下已写过的同一内容再次排入会重新进入待写集合。",
			Parameters:  toolexec.SchemaFor[queueAlbumWritebackArgs](),
			Execute: toolexec.Typed(func(ctx *toolexec.ToolExecContext, args queueAlbumWritebackArgs) (sdk.ToolOutput, error) {
				if p.service == nil {
					return sdk.ToolOutput{}, errAlbumWritebackUnavailable
				}
				userID := strings.TrimSpace(sess.UserID)
				botID := strings.TrimSpace(sess.BotID)
				if userID == "" || botID == "" {
					return sdk.ToolOutput{}, errors.New("album write-back needs an owning user and bot")
				}
				result, err := p.service.Enqueue(ctx.Context, userID, botID, albumwriteback.EnqueueInput{
					PairKey:       args.PairKey,
					ContentHashes: args.ContentHashes,
					Name:          args.Name,
				})
				if err != nil {
					return sdk.ToolOutput{}, albumWritebackToolError(err)
				}
				return toolexec.OutputFromValue(map[string]any{
					"ok":          true,
					"pair_key":    result.PairKey,
					"enqueued":    result.Enqueued,
					"enqueued_n":  len(result.Enqueued),
					"written_now": false,
				}), nil
			}),
		},
	}, nil
}

func albumWritebackToolError(err error) error {
	switch {
	case errors.Is(err, albumwriteback.ErrInvalid):
		return errors.New("参数无效：pair_key 只能用字母、数字、下划线、连字符或点，content_hashes 必须是 64 位小写十六进制摘要")
	case errors.Is(err, albumwriteback.ErrAssetUnavailable):
		return errors.New("有 media 不属于本 Bot 或已不存在，本次未排入任何项目；请用处理结果中的 content_hash 重试")
	default:
		return err
	}
}

type queueAlbumWritebackArgs struct {
	PairKey       string   `json:"pair_key" jsonschema:"用户为手机相册指定的配对标签，例如 processed-default。"`
	ContentHashes []string `json:"content_hashes" jsonschema:"已处理完成媒体的 content_hash（64 位小写十六进制），一项写回相册一次。"`
	Name          string   `json:"name,omitempty" jsonschema:"可选：这一批文件在手机上的显示名。"`
}
