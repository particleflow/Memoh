package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/felinics/memoh/internal/storage"
)

// Service provides content-addressed media asset persistence.
// All metadata is derived from the filesystem — no database, no sidecar files.
type Service struct {
	provider              storage.Provider
	logger                *slog.Logger
	requirePersistence    bool
	maxUploadBytes        int64
	uploads               *UploadStore
	transferNetworkPolicy *TransferNetworkPolicy
}

// SetMaxUploadBytes configures HTTP attachment admission. Zero keeps the default.
func (s *Service) SetMaxUploadBytes(limit int64) { s.maxUploadBytes = limit }

func (s *Service) MaxUploadBytes() int64 {
	if s.maxUploadBytes > 0 {
		return s.maxUploadBytes
	}
	return MaxAssetBytes
}

// SetRequirePersistence makes attachment admission fail if an original cannot
// be persisted or resolved. It also applies when storage is reached over RPC.
func (s *Service) SetRequirePersistence(required bool) { s.requirePersistence = required }

func (s *Service) RequiresPersistence() bool { return s != nil && s.requirePersistence }

// NewService creates a media service with the given storage provider.
func NewService(log *slog.Logger, provider storage.Provider) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		provider: provider,
		logger:   log.With(slog.String("service", "media")),
	}
}

// Ingest persists a new media asset. It hashes the content, deduplicates by
// checking the filesystem, and stores the bytes. Returns a derived Asset.
func (s *Service) Ingest(ctx context.Context, input IngestInput) (Asset, error) {
	if s.provider == nil {
		return Asset{}, ErrProviderUnavailable
	}
	if !validPathComponent(input.BotID) {
		return Asset{}, errors.New("bot id is required")
	}
	if input.Reader == nil {
		return Asset{}, errors.New("reader is required")
	}

	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxAssetBytes
	}
	contentHash, sizeBytes, tempFile, err := spoolAndHashWithLimit(contextReader{ctx, input.Reader}, maxBytes)
	if err != nil {
		return Asset{}, fmt.Errorf("read input: %w", err)
	}
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name()) //nolint:gosec // G703: path is from os.CreateTemp, not from user input
	}()

	return s.persist(ctx, input, contentHash, sizeBytes, tempFile)
}

func (s *Service) persist(ctx context.Context, input IngestInput, contentHash string, sizeBytes int64, reader io.Reader) (Asset, error) {
	mime := coalesce(input.Mime, "application/octet-stream")
	ext := extensionFromMime(mime)
	if ext == ".bin" && input.OriginalExt != "" {
		ext = input.OriginalExt
	}
	if !validPathComponent(ext) || !strings.HasPrefix(ext, ".") || strings.Count(ext, ".") != 1 {
		return Asset{}, errors.New("invalid file extension")
	}
	storageKey := path.Join(contentHash[:2], contentHash+ext)
	routingKey := path.Join(input.BotID, storageKey)

	// Filesystem dedup: if the file already exists, skip write.
	if existing, openErr := s.provider.Open(ctx, routingKey); openErr == nil {
		_ = existing.Close()
		if ensurer, ok := s.provider.(storage.DurabilityEnsurer); ok {
			if err := ensurer.EnsureDurable(ctx, routingKey); err != nil {
				return Asset{}, fmt.Errorf("confirm existing media durability: %w", err)
			}
		} else if s.requirePersistence {
			// Providers reached over a transport may not expose a repair barrier.
			// Republish through their successful-Put contract instead of treating
			// readability alone as a durable acknowledgement.
			if err := s.provider.Put(ctx, routingKey, reader); err != nil {
				return Asset{}, fmt.Errorf("republish existing media: %w", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return Asset{}, err
		}
		return Asset{
			ContentHash: contentHash,
			BotID:       input.BotID,
			Mime:        mime,
			SizeBytes:   sizeBytes,
			StorageKey:  storageKey,
		}, nil
	}

	if err := s.provider.Put(ctx, routingKey, reader); err != nil {
		return Asset{}, fmt.Errorf("store media: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}

	return Asset{
		ContentHash: contentHash,
		BotID:       input.BotID,
		Mime:        mime,
		SizeBytes:   sizeBytes,
		StorageKey:  storageKey,
	}, nil
}

// Resolve finds an asset by content hash (no stream open). Used to fill mime/storage_key when DB has none.
func (s *Service) Resolve(ctx context.Context, botID, contentHash string) (Asset, error) {
	if s.provider == nil {
		return Asset{}, ErrProviderUnavailable
	}
	return s.resolveByContentHash(ctx, botID, contentHash)
}

// Stat returns asset metadata for the given content hash without opening the file.
// It satisfies the channel.OutboundAttachmentStore interface.
func (s *Service) Stat(ctx context.Context, botID, contentHash string) (Asset, error) {
	return s.Resolve(ctx, botID, contentHash)
}

// Open returns a reader for the media asset identified by content hash.
// It locates the file by scanning extensions under the hash prefix and derives MIME from the extension.
func (s *Service) Open(ctx context.Context, botID, contentHash string) (io.ReadCloser, Asset, error) {
	if s.provider == nil {
		return nil, Asset{}, ErrProviderUnavailable
	}
	asset, err := s.resolveByContentHash(ctx, botID, contentHash)
	if err != nil {
		return nil, Asset{}, err
	}
	routingKey := path.Join(botID, asset.StorageKey)
	reader, err := s.provider.Open(ctx, routingKey)
	if err != nil {
		return nil, Asset{}, fmt.Errorf("open storage: %w", err)
	}
	return reader, asset, nil
}

// GetByStorageKey returns an asset derived from a known storage key.
func (s *Service) GetByStorageKey(ctx context.Context, botID, storageKey string) (Asset, error) {
	if s.provider == nil {
		return Asset{}, ErrProviderUnavailable
	}
	if !validPathComponent(botID) || !validStorageKey(storageKey) {
		return Asset{}, ErrAssetNotFound
	}
	routingKey := path.Join(botID, storageKey)
	rc, err := s.provider.Open(ctx, routingKey)
	if err != nil {
		return Asset{}, ErrAssetNotFound
	}
	_ = rc.Close()
	return deriveAssetFromKey(botID, storageKey), nil
}

// AccessPath returns a reachable consumer reference for a persisted asset.
// Providers that support materialization may promote spill storage into their
// consumer-addressable primary store. Errors are intentionally collapsed for
// legacy callers; new routing code should use EnsureAccessPath.
func (s *Service) AccessPath(ctx context.Context, asset Asset) string {
	accessPath, _ := s.EnsureAccessPath(ctx, asset)
	return accessPath
}

// EnsureAccessPath returns a consumer-visible path, materializing the asset
// into addressable storage when the provider supports it.
func (s *Service) EnsureAccessPath(ctx context.Context, asset Asset) (string, error) {
	if s.provider == nil {
		return "", ErrProviderUnavailable
	}
	if !validPathComponent(asset.BotID) || !validStorageKey(asset.StorageKey) {
		return "", ErrAssetNotFound
	}
	routingKey := path.Join(asset.BotID, asset.StorageKey)
	if ensurer, ok := s.provider.(storage.AccessPathEnsurer); ok {
		accessPath, err := ensurer.EnsureAccessPath(ctx, routingKey)
		if err != nil {
			return "", err
		}
		accessPath = strings.TrimSpace(accessPath)
		if accessPath == "" {
			return "", storage.ErrAccessPathUnavailable
		}
		return accessPath, nil
	}
	accessPath := strings.TrimSpace(s.provider.AccessPath(ctx, routingKey))
	if accessPath == "" {
		return "", storage.ErrAccessPathUnavailable
	}
	return accessPath, nil
}

// IngestContainerFile reads an arbitrary file from a bot's /data/ directory
// and ingests it into the media store. The provider must implement ContainerFileOpener.
func (s *Service) IngestContainerFile(ctx context.Context, botID, containerPath string) (Asset, error) {
	if s.provider == nil {
		return Asset{}, ErrProviderUnavailable
	}
	opener, ok := s.provider.(storage.ContainerFileOpener)
	if !ok {
		return Asset{}, storage.ErrContainerFileNotSupported
	}
	f, err := opener.OpenContainerFile(ctx, botID, containerPath)
	if err != nil {
		return Asset{}, fmt.Errorf("open workspace file: %w", err)
	}
	defer func() { _ = f.Close() }()
	ext := path.Ext(containerPath)
	mime := mimeFromExtension(ext)
	return s.Ingest(ctx, IngestInput{BotID: botID, Mime: mime, Reader: f, OriginalExt: ext})
}

// resolveByContentHash scans hash-prefix directory by extension to find the file.
// It first tries known extensions (fast path), then falls back to a directory
// listing if the provider supports it, so arbitrary file types are found.
func (s *Service) resolveByContentHash(ctx context.Context, botID, contentHash string) (Asset, error) {
	if !validPathComponent(botID) || !validPathComponent(contentHash) || len(contentHash) < 2 {
		return Asset{}, ErrAssetNotFound
	}
	prefix := contentHash[:2]

	if s.requirePersistence {
		// Canonical originals use lowercase SHA-256 keys. Classify malformed
		// references before provider validation can turn them into storage errors.
		if len(contentHash) != hex.EncodedLen(sha256.Size) || contentHash != strings.ToLower(contentHash) {
			return Asset{}, ErrAssetNotFound
		}
		if _, err := hex.DecodeString(contentHash); err != nil {
			return Asset{}, ErrAssetNotFound
		}
		if lister, ok := s.provider.(storage.PrefixLister); ok {
			keys, err := lister.ListPrefix(ctx, path.Join(botID, prefix, contentHash))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return Asset{}, ErrAssetNotFound
				}
				return Asset{}, fmt.Errorf("find original: %w", err)
			}
			for _, key := range keys {
				owner, storageKey := splitFirst(key, '/')
				asset := deriveAssetFromKey(botID, storageKey)
				if owner == botID && validStorageKey(storageKey) && asset.ContentHash == contentHash {
					return asset, nil
				}
			}
			return Asset{}, ErrAssetNotFound
		}
	}

	for _, ext := range knownExtensions {
		storageKey := path.Join(prefix, contentHash+ext)
		routingKey := path.Join(botID, storageKey)
		rc, err := s.provider.Open(ctx, routingKey)
		if err != nil {
			continue
		}
		_ = rc.Close()
		return deriveAssetFromKey(botID, storageKey), nil
	}

	if lister, ok := s.provider.(storage.PrefixLister); ok {
		keyPrefix := path.Join(botID, prefix, contentHash)
		keys, err := lister.ListPrefix(ctx, keyPrefix)
		if err == nil {
			for _, k := range keys {
				_, storageKey := splitFirst(k, '/')
				if storageKey != "" {
					return deriveAssetFromKey(botID, storageKey), nil
				}
			}
		}
	}

	return Asset{}, ErrAssetNotFound
}

func validPathComponent(value string) bool {
	return strings.TrimSpace(value) != "" && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\\x00")
}

func validStorageKey(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && validPathComponent(parts[0]) && validPathComponent(parts[1])
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(b)
}

func splitFirst(s string, sep byte) (string, string) {
	i := strings.IndexByte(s, sep)
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// deriveAssetFromKey builds an Asset from the storage key (hash_2char_prefix/hash.ext).
func deriveAssetFromKey(botID, storageKey string) Asset {
	base := path.Base(storageKey)
	ext := path.Ext(base)
	hash := strings.TrimSuffix(base, ext)
	return Asset{
		ContentHash: hash,
		BotID:       botID,
		Mime:        mimeFromExtension(ext),
		StorageKey:  storageKey,
	}
}

var extToMime = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".flac": "audio/flac", ".aac": "audio/aac",
	".mp4": "video/mp4", ".webm": "video/webm", ".avi": "video/x-msvideo", ".mov": "video/quicktime",
	".pdf": "application/pdf", ".zip": "application/zip", ".gz": "application/gzip",
	".json": "application/json", ".xml": "application/xml", ".csv": "text/csv",
	".txt": "text/plain", ".md": "text/markdown", ".log": "text/plain",
	".html": "text/html", ".css": "text/css",
	".js": "text/javascript", ".ts": "text/typescript",
	".py": "text/x-python", ".go": "text/x-go", ".rs": "text/x-rust",
	".c": "text/x-c", ".cpp": "text/x-c++", ".h": "text/x-c",
	".java": "text/x-java", ".rb": "text/x-ruby", ".sh": "text/x-shellscript",
	".yaml": "text/yaml", ".yml": "text/yaml", ".toml": "text/toml",
	".sql": "text/x-sql", ".ini": "text/plain", ".conf": "text/plain",
}

var mimeToExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif",
	"image/webp": ".webp", "image/svg+xml": ".svg",
	"audio/mpeg": ".mp3", "audio/wav": ".wav", "audio/ogg": ".ogg",
	"audio/flac": ".flac", "audio/aac": ".aac",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/x-msvideo": ".avi", "video/quicktime": ".mov",
	"application/pdf": ".pdf", "application/zip": ".zip", "application/gzip": ".gz",
	"application/json": ".json", "application/xml": ".xml",
	"text/plain": ".txt", "text/markdown": ".md", "text/csv": ".csv",
	"text/html": ".html", "text/css": ".css",
	"text/javascript": ".js", "text/typescript": ".ts",
	"text/x-python": ".py", "text/x-go": ".go", "text/x-rust": ".rs",
	"text/x-c": ".c", "text/x-c++": ".cpp",
	"text/x-java": ".java", "text/x-ruby": ".rb", "text/x-shellscript": ".sh",
	"text/yaml": ".yaml", "text/toml": ".toml", "text/x-sql": ".sql",
}

var knownExtensions []string

func init() {
	seen := make(map[string]bool)
	for ext := range extToMime {
		if !seen[ext] {
			knownExtensions = append(knownExtensions, ext)
			seen[ext] = true
		}
	}
	if !seen[".bin"] {
		knownExtensions = append(knownExtensions, ".bin")
	}
}

func mimeFromExtension(ext string) string {
	if mime, ok := extToMime[strings.ToLower(ext)]; ok {
		return mime
	}
	return "application/octet-stream"
}

// MimeFromPath derives MIME from an already-persisted filename or storage key.
// It is deliberately metadata-only: history reads can use it without opening
// workspace storage or waking a runtime provider.
func MimeFromPath(filePath string) string {
	ext := path.Ext(strings.TrimSpace(filePath))
	if ext == "" {
		return ""
	}
	mime := mimeFromExtension(ext)
	if mime == "application/octet-stream" {
		return ""
	}
	return mime
}

func extensionFromMime(mime string) string {
	if ext, ok := mimeToExt[strings.ToLower(strings.TrimSpace(mime))]; ok {
		return ext
	}
	return ".bin"
}

func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// spoolAndHashWithLimit streams reader into a temp file while computing its SHA-256.
// Returns the open file sought to the beginning; caller must close and remove it.
func spoolAndHashWithLimit(reader io.Reader, maxBytes int64) (contentHash string, size int64, f *os.File, err error) {
	if reader == nil {
		return "", 0, nil, errors.New("reader is required")
	}
	if maxBytes <= 0 {
		return "", 0, nil, errors.New("max bytes must be greater than 0")
	}
	tmp, createErr := os.CreateTemp("", "memoh-media-*")
	if createErr != nil {
		return "", 0, nil, fmt.Errorf("create temp file: %w", createErr)
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) //nolint:gosec // G703: path is from os.CreateTemp, not from user input
	}

	hasher := sha256.New()
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if copyErr != nil {
		cleanup()
		return "", 0, nil, fmt.Errorf("copy to temp file: %w", copyErr)
	}
	if written > maxBytes {
		cleanup()
		return "", 0, nil, fmt.Errorf("%w: max %d bytes", ErrAssetTooLarge, maxBytes)
	}
	if written == 0 {
		cleanup()
		return "", 0, nil, errors.New("asset payload is empty")
	}
	if _, seekErr := tmp.Seek(0, io.SeekStart); seekErr != nil {
		cleanup()
		return "", 0, nil, fmt.Errorf("seek temp file: %w", seekErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, tmp, nil
}
