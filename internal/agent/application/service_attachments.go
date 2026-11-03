package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/felinics/memoh/internal/apperror"
	attachmentpkg "github.com/felinics/memoh/internal/attachment"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/storage"
)

const (
	gatewayInlineAttachmentMaxBytes int64 = 20 * 1024 * 1024

	// attachmentPreparationTimeout bounds preparing one turn's attachments.
	// Rendering an animated sticker has its own, tighter deadline; this is the
	// ceiling on a turn that carries a lot of them, so a batch of pathological
	// media cannot hold a conversation open indefinitely.
	attachmentPreparationTimeout = 30 * time.Second
)

// routeAndMergeAttachments applies CapabilityFallbackPolicy to split
// request attachments by model input modalities, then merges the results
// into a single []any for the gateway request.
func (s *Service) routeAndMergeAttachments(ctx context.Context, model models.GetResponse, req ChatRequest) ([]any, error) {
	if len(req.Attachments) == 0 && len(req.ReplyAttachments) == 0 {
		return []any{}, nil
	}
	typed, err := s.prepareGatewayAttachments(ctx, req, modelAcceptsImages(model))
	if err != nil {
		return nil, err
	}
	routed := routeAttachmentsByCapability(model.Config.Compatibilities, typed)
	for i := range routed.Fallback {
		fallbackPath := strings.TrimSpace(routed.Fallback[i].FallbackPath)
		if fallbackPath == "" {
			// The attachment passed admission, and now neither a payload nor a
			// path can reach the model. Answering without it would tell the
			// model the user sent an empty message; the turn fails instead.
			if err := routed.Fallback[i].Unreachable; err != nil {
				return nil, attachmentUnreachableError(err)
			}
			if s != nil && s.logger != nil {
				s.logger.WarnContext(ctx,
					"drop attachment without fallback path",
					slog.String("type", strings.TrimSpace(routed.Fallback[i].Type)),
					slog.String("transport", strings.TrimSpace(routed.Fallback[i].Transport)),
					slog.String("content_hash", strings.TrimSpace(routed.Fallback[i].ContentHash)),
					slog.Bool("has_payload", strings.TrimSpace(routed.Fallback[i].Payload) != ""),
				)
			}
			routed.Fallback[i] = gatewayAttachment{}
			continue
		}
		routed.Fallback[i].Type = "file"
		routed.Fallback[i].Transport = gatewayTransportToolFileRef
		routed.Fallback[i].Payload = fallbackPath
	}
	// With the inline content channels closed, the header reference row is the
	// only lane that reaches the model. A natively-routed attachment without a
	// resolvable access path therefore has no lane at all: fail the turn the
	// same way the fallback lane does instead of silently dropping it.
	for i := range routed.Native {
		if strings.TrimSpace(routed.Native[i].FallbackPath) != "" {
			continue
		}
		if err := routed.Native[i].Unreachable; err != nil {
			return nil, attachmentUnreachableError(err)
		}
		if s != nil && s.logger != nil {
			s.logger.WarnContext(ctx,
				"drop attachment without any reference path",
				slog.String("type", strings.TrimSpace(routed.Native[i].Type)),
				slog.String("transport", strings.TrimSpace(routed.Native[i].Transport)),
				slog.String("content_hash", strings.TrimSpace(routed.Native[i].ContentHash)),
				slog.Bool("has_payload", strings.TrimSpace(routed.Native[i].Payload) != ""),
			)
		}
		routed.Native[i] = gatewayAttachment{}
	}
	merged := make([]any, 0, len(routed.Native)+len(routed.Fallback))
	for _, att := range routed.Native {
		if att.Type == "" || strings.TrimSpace(att.Transport) == "" {
			continue
		}
		merged = append(merged, att)
	}
	for _, fb := range routed.Fallback {
		if fb.Type == "" || strings.TrimSpace(fb.Transport) == "" || strings.TrimSpace(fb.Payload) == "" {
			continue
		}
		merged = append(merged, fb)
	}
	if len(merged) == 0 {
		return []any{}, nil
	}
	return merged, nil
}

// prepareGatewayAttachments materializes request attachments for the gateway.
//
// An attachment that carries a content hash has already passed admission, where
// the original was opened successfully. If it can no longer be read here, the
// turn must fail: answering without the file tells the model and the user that
// an attachment they sent is simply absent, which is worse than an explicit
// error they can act on.
//
// acceptsImages says whether inline images can be used at all. Preparing an
// image is not free — an animated sticker is rasterised, any stored image is
// read and encoded — and a model without vision routes every one of them to a
// file reference regardless, so the work is skipped rather than thrown away.
func (s *Service) prepareGatewayAttachments(ctx context.Context, req ChatRequest, acceptsImages bool) ([]gatewayAttachment, error) {
	ctx, cancel := context.WithTimeout(ctx, attachmentPreparationTimeout)
	defer cancel()
	attachments := requestAttachmentsForGateway(req)
	if len(attachments) == 0 {
		return nil, nil
	}
	prepared := make([]gatewayAttachment, 0, len(attachments))
	for _, raw := range attachments {
		bundle := raw.Bundle()
		attachmentType := strings.ToLower(strings.TrimSpace(bundle.Type))
		payload := strings.TrimSpace(bundle.Base64)
		transport := ""
		fallbackPath := strings.TrimSpace(bundle.Path)
		rawURL := strings.TrimSpace(bundle.URL)
		if payload != "" {
			transport = gatewayTransportInlineDataURL
			if fallbackPath == "" && rawURL != "" {
				fallbackPath = rawURL
			}
		} else {
			contentHash := strings.TrimSpace(bundle.ContentHash)
			switch {
			case isLikelyPublicURL(rawURL) && contentHash == "":
				// Only treat a public HTTP URL as direct vision input when the
				// attachment has not been persisted yet. If ContentHash is set,
				// the file is already in the media store and will be inlined
				// by inlineImageAttachmentAssetIfNeeded below — prefer that path
				// so we never expose ephemeral or credentialed platform URLs
				// directly to the model.
				payload = rawURL
				transport = gatewayTransportPublicURL
			case rawURL != "" && fallbackPath == "":
				// URL is either a persisted local path (contentHash set) or an
				// unresolvable reference; store it as fallbackPath so the agent
				// can access it via the file tool if needed.
				fallbackPath = rawURL
			}
		}
		item := gatewayAttachment{
			ContentHash:  strings.TrimSpace(bundle.ContentHash),
			Type:         attachmentType,
			Mime:         strings.TrimSpace(bundle.Mime),
			Size:         bundle.Size,
			Name:         strings.TrimSpace(bundle.Name),
			Transport:    transport,
			Payload:      payload,
			Metadata:     bundle.Metadata,
			FallbackPath: fallbackPath,
		}
		if item.ContentHash != "" && strings.TrimSpace(item.FallbackPath) == "" {
			accessPath, err := s.assetAccessPath(ctx, strings.TrimSpace(req.BotID), item.ContentHash)
			if err != nil {
				if s != nil && s.logger != nil {
					s.logger.WarnContext(ctx,
						"resolve gateway attachment access path failed",
						slog.Any("error", err),
						slog.String("bot_id", strings.TrimSpace(req.BotID)),
						slog.String("content_hash", item.ContentHash),
					)
				}
				// A path is only one carrier. Whether this attachment reaches
				// the model at all is decided once every carrier has been tried.
				item.Unreachable = err
			} else {
				item.FallbackPath = accessPath
			}
		}
		item = normalizeGatewayAttachmentPayload(item)
		if acceptsImages {
			item = s.inlineImageAttachmentAssetIfNeeded(ctx, strings.TrimSpace(req.BotID), item)
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

// attachmentUnreachableError names why an admitted attachment can no longer be
// read, so the client learns whether to resend the file or retry the turn. The
// distinction comes from the read itself: bytes that are gone, bytes this
// process may not read, and any other read failure are three different answers.
func attachmentUnreachableError(err error) error {
	switch {
	case errors.Is(err, media.ErrAssetNotFound), errors.Is(err, fs.ErrNotExist):
		return apperror.Wrap(apperror.CodeMediaAttachmentGone, err, nil)
	case errors.Is(err, storage.ErrAccessPathUnavailable):
		return apperror.Wrap(apperror.CodeMediaAttachmentUnreadable, err, nil)
	default:
		return apperror.Wrap(apperror.CodeMediaAttachmentUnavailable, err, nil)
	}
}

func (s *Service) assetAccessPath(ctx context.Context, botID, contentHash string) (string, error) {
	if s == nil || s.assetLoader == nil {
		return "", errors.New("gateway asset loader not configured")
	}
	accessPath, err := s.assetLoader.AccessPathForGateway(ctx, botID, contentHash)
	if err != nil {
		return "", err
	}
	accessPath = strings.TrimSpace(accessPath)
	if accessPath == "" {
		return "", errors.New("asset has no reachable access path")
	}
	return accessPath, nil
}

func requestAttachmentsForGateway(req ChatRequest) []ChatAttachment {
	if len(req.ReplyAttachments) == 0 {
		return req.Attachments
	}
	attachments := make([]ChatAttachment, 0, len(req.Attachments)+len(req.ReplyAttachments))
	attachments = append(attachments, req.Attachments...)
	attachments = append(attachments, req.ReplyAttachments...)
	return attachments
}

func normalizeGatewayAttachmentPayload(item gatewayAttachment) gatewayAttachment {
	if item.Transport != gatewayTransportInlineDataURL {
		return item
	}
	payload := strings.TrimSpace(item.Payload)
	if payload == "" {
		return item
	}
	if strings.HasPrefix(strings.ToLower(payload), "data:") {
		mime := strings.TrimSpace(item.Mime)
		if mime == "" || strings.EqualFold(mime, "application/octet-stream") {
			if extracted := attachmentpkg.MimeFromDataURL(payload); extracted != "" {
				item.Mime = extracted
			}
		}
		item.Payload = payload
		return item
	}
	mime := strings.TrimSpace(item.Mime)
	if mime == "" {
		mime = "application/octet-stream"
	}
	item.Payload = attachmentpkg.NormalizeBase64DataURL(payload, mime)
	return item
}

func isLikelyPublicURL(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")
}

// splitInlineDataURL strips a data URL down to bare base64 plus the header
// mime. FilePart.Data carries bare base64 by convention; provider framing is
// the adapters' job.
func splitInlineDataURL(payload string) (string, string) {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(strings.ToLower(payload), "data:") {
		return payload, ""
	}
	idx := strings.Index(payload, ",")
	if idx < 0 {
		return payload, ""
	}
	header := payload[len("data:"):idx]
	if semi := strings.Index(header, ";"); semi >= 0 {
		header = header[:semi]
	}
	return payload[idx+1:], strings.TrimSpace(header)
}

func (s *Service) inlineImageAttachmentAssetIfNeeded(ctx context.Context, botID string, item gatewayAttachment) gatewayAttachment {
	if item.Type != "image" {
		return item
	}
	if strings.TrimSpace(item.Payload) != "" && item.Transport == gatewayTransportInlineDataURL {
		// Bytes are already here, so check them rather than trusting the label
		// the uploader attached to them.
		payload, mime, err := normalizeInlineImageDataURL(item.Payload)
		if err != nil {
			s.logImageInputRejected(err, botID, item.ContentHash)
			return demoteNonImageAttachment(item)
		}
		item.Payload, item.Mime = payload, mime
		return item
	}
	if strings.TrimSpace(item.Payload) != "" && item.Transport == gatewayTransportPublicURL {
		// Remote bytes cannot be sniffed here; the raster allowlist on the
		// declared MIME is all the capability router has to go on.
		return item
	}
	contentHash := strings.TrimSpace(item.ContentHash)
	if contentHash == "" {
		return item
	}
	dataURLs, mime, err := s.inlineStoredImageDataURLs(ctx, botID, contentHash, item.Mime)
	if err != nil {
		s.logImageInputRejected(err, botID, contentHash)
		if errors.Is(err, errUnsupportedImageBytes) {
			return demoteNonImageAttachment(item)
		}
		// A stored image that cannot be read has no bytes to inline. With a
		// reachable access path the agent can still read the original, so the
		// turn is only at risk when no carrier resolved at all.
		if strings.TrimSpace(item.FallbackPath) == "" {
			item.Unreachable = err
		}
		return item
	}
	item.Transport = gatewayTransportInlineDataURL
	item.Payload = dataURLs[0]
	item.Mime = mime
	if len(dataURLs) > 1 {
		item.Frames = dataURLs
		// Size drives the per-request media budget, so it has to account for
		// every frame the model will actually receive.
		item.Size = totalInlineDataURLBytes(dataURLs)
	}
	return item
}

// totalInlineDataURLBytes estimates the decoded size of a set of data URLs.
func totalInlineDataURLBytes(dataURLs []string) int64 {
	var total int64
	for _, dataURL := range dataURLs {
		body, _ := splitInlineDataURL(dataURL)
		total += int64(len(body)) * 3 / 4
	}
	return total
}

// demoteNonImageAttachment keeps media the model cannot parse out of the vision
// lane. The capability router turns it into a file reference, so the agent can
// still reach the original through workspace tools.
func demoteNonImageAttachment(item gatewayAttachment) gatewayAttachment {
	item.Mime = "application/octet-stream"
	return item
}

// logImageInputRejected records why an attachment was withheld from vision
// input. content_hash is the handle support needs to find the stored original;
// the bytes themselves and any credentialed source URL stay out of the log.
func (s *Service) logImageInputRejected(err error, botID, contentHash string) {
	if s == nil || s.logger == nil {
		return
	}
	s.logger.Warn(
		"attachment withheld from vision input; original still reachable as a file",
		slog.Any("error", err),
		slog.String("bot_id", strings.TrimSpace(botID)),
		slog.String("content_hash", strings.TrimSpace(contentHash)),
	)
}

func encodeReaderAsDataURL(reader io.Reader, maxBytes int64, attachmentType, fallbackMime string) (string, string, error) {
	if reader == nil {
		return "", "", errors.New("reader is required")
	}
	if maxBytes <= 0 {
		return "", "", errors.New("max bytes must be greater than 0")
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	head := make([]byte, 512)
	// A Reader may return fewer bytes than asked for without being at the end
	// of the stream, and a short prefix cannot be told apart from arbitrary
	// binary. Fill the sniff window before deciding anything about the format.
	n, err := io.ReadFull(limited, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", "", fmt.Errorf("read asset: %w", err)
	}
	head = head[:n]

	mime := strings.TrimSpace(fallbackMime)
	if strings.EqualFold(strings.TrimSpace(attachmentType), "image") {
		detected, err := modelImageMimeFromBytes(head)
		if err != nil {
			return "", "", err
		}
		mime = detected
	}
	if mime == "" {
		mime = "application/octet-stream"
	}

	var encoded strings.Builder
	encoded.Grow(len("data:") + len(mime) + len(";base64,"))
	encoded.WriteString("data:")
	encoded.WriteString(mime)
	encoded.WriteString(";base64,")

	encoder := base64.NewEncoder(base64.StdEncoding, &encoded)
	if len(head) > 0 {
		if _, err := encoder.Write(head); err != nil {
			_ = encoder.Close()
			return "", "", fmt.Errorf("encode asset head: %w", err)
		}
	}
	copied, err := io.Copy(encoder, limited)
	if err != nil {
		_ = encoder.Close()
		return "", "", fmt.Errorf("encode asset body: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", "", fmt.Errorf("finalize asset encoding: %w", err)
	}

	total := int64(len(head)) + copied
	if total > maxBytes {
		return "", "", fmt.Errorf(
			"asset too large to inline: %d > %d",
			total,
			maxBytes,
		)
	}
	return encoded.String(), mime, nil
}
