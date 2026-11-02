package handlers

import (
	"context"
	"sync"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	audiopkg "github.com/felinics/memoh/internal/audio"
	"github.com/felinics/memoh/internal/audio/input"
)

const (
	// playbackReuseLimit bounds the bytes buffered while a task is recorded or a
	// reply is replayed. A larger reply is never registered and never served
	// from cache: holding it in memory would trade seconds of provider latency
	// for unbounded heap, and skipping reuse only costs one synthesis.
	playbackReuseLimit = 8 << 20

	// playbackReuseIdle is how long a deferred input collects the client's text
	// before concluding the reply is still streaming. A replay feeds every
	// chunk back to back, so a real hit resolves in milliseconds; a live reply
	// leaves gaps between sentences and then synthesizes exactly as before.
	playbackReuseIdle = 400 * time.Millisecond

	// playbackReuseWriteTimeout bounds registering a finished utterance. The
	// write is detached from the request so a disconnect cannot skip it, but it
	// must never outlive the response by much.
	playbackReuseWriteTimeout = 5 * time.Second
)

// playbackReuseStore is the durable reuse surface the handler needs. Keeping it
// narrow lets tests use an in-memory fixture without a database.
type playbackReuseStore interface {
	Candidates(context.Context, audiopkg.PlaybackReuseKey) []audiopkg.CachedPlayback
	Store(context.Context, audiopkg.PlaybackReuseKey, []byte, string, string) error
}

// playbackReuseInfoService resolves the reuse identity without dialing.
type playbackReuseInfoService interface {
	PlaybackReuseInfo(ctx context.Context, botID, modelID, text string) (audiopkg.PlaybackReuseKey, string, string, error)
}

// playbackReuseRecorder accumulates the exact spoken text of one task and the
// audio bytes it produced. Registration requires closed input and no overflow:
// a partial utterance must never become a hit, or replaying it would silently
// drop the rest of the reply.
type playbackReuseRecorder struct {
	mu       sync.Mutex
	store    playbackReuseStore
	key      audiopkg.PlaybackReuseKey
	text     []byte
	audio    []byte
	count    uint64
	offset   uint64
	final    bool
	overflow bool
}

func newPlaybackReuseRecorder(store playbackReuseStore, key audiopkg.PlaybackReuseKey) *playbackReuseRecorder {
	if store == nil || key.PrefixHash == "" {
		return nil
	}
	return &playbackReuseRecorder{store: store, key: key}
}

// record mirrors one frame. A nil recorder (reuse disabled) ignores it, so the
// caller never has to branch on whether reuse is on.
func (r *playbackReuseRecorder) record(frame input.Frame) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overflow || r.final || frame.Sequence != r.count || frame.Offset != r.offset {
		r.overflow = true
		return
	}
	if len(r.text)+len(frame.Text) > playbackReuseLimit {
		r.overflow = true
		return
	}
	r.text = append(r.text, frame.Text...)
	r.count++
	r.offset += uint64(len([]rune(frame.Text)))
	r.final = frame.Final
}

func (r *playbackReuseRecorder) recordAudio(chunk []byte) {
	if r == nil || len(chunk) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overflow {
		return
	}
	if len(r.audio)+len(chunk) > playbackReuseLimit {
		r.overflow = true
		r.audio = nil
		return
	}
	r.audio = append(r.audio, chunk...)
}

// commit registers the finished utterance under its whole-text identity. It is
// a no-op unless input closed cleanly, and it never fails the response: reuse
// is an optimization, so a storage error only costs the next synthesis.
func (r *playbackReuseRecorder) commit(ctx context.Context, contentType string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	key, audio, text, ok := r.key, r.audio, string(r.text), r.final && !r.overflow && len(r.audio) > 0
	r.mu.Unlock()
	if !ok || contentType == "" {
		return
	}
	_ = r.store.Store(ctx, key.WithUtterance(text), audio, contentType, "")
}

// playbackRecordedSession mirrors every frame into the recorder so the whole
// utterance can be registered once the provider finishes. It adds no behavior
// to the underlying session.
type playbackRecordedSession struct {
	input.Session
	recorder *playbackReuseRecorder
}

func (s *playbackRecordedSession) Append(ctx context.Context, frame input.Frame) error {
	if err := s.Session.Append(ctx, frame); err != nil {
		return err
	}
	// A nil recorder means reuse is disabled for this request; the frame was
	// still delivered to the provider above.
	if s.recorder != nil {
		s.recorder.record(frame)
	}
	return nil
}

// playbackReuseSession is one POST's speech input when a finished utterance may
// already exist for its opening frames.
//
// The native contract forces the shape: the client only PUTs the rest of its
// text after the POST response headers arrive, so the server cannot know the
// whole utterance at POST time. This session therefore publishes readiness
// immediately, collects the client's frames, and decides once input closes or
// the idle bound expires. A match streams the stored bytes and never dials a
// provider; anything else opens one and replays every accepted frame in order,
// which is the pre-existing path.
type playbackReuseSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	open     func(context.Context) (input.Session, string, error)
	key      audiopkg.PlaybackReuseKey
	cached   []audiopkg.CachedPlayback
	recorder *playbackReuseRecorder
	stream   *sdk.SpeechStreamResult
	chunks   chan []byte
	errs     chan error
	frames   chan input.Frame
}

func newPlaybackReuseSession(
	ctx context.Context,
	open func(context.Context) (input.Session, string, error),
	key audiopkg.PlaybackReuseKey,
	cached []audiopkg.CachedPlayback,
	recorder *playbackReuseRecorder,
	contentType string,
) *playbackReuseSession {
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &playbackReuseSession{
		ctx: sessionCtx, cancel: cancel, open: open, key: key, cached: cached, recorder: recorder,
		chunks: make(chan []byte, 8), errs: make(chan error, 1), frames: make(chan input.Frame, 32),
	}
	s.stream = sdk.NewSpeechStreamResult(s.chunks, contentType, s.errs)
	go func() {
		defer close(s.errs) // Err() blocks until this channel closes; a nil value is a clean end.
		defer close(s.chunks)
		s.pump()
	}()
	return s
}

func (s *playbackReuseSession) Audio() *sdk.SpeechStreamResult { return s.stream }

// Cancel closes the session and any provider task it opened.
func (s *playbackReuseSession) Cancel() { s.cancel() }

// Append queues the frame for the pump, which is the only place a provider is
// opened. Backpressure is bounded; a client that floods input without reading
// audio simply waits.
func (s *playbackReuseSession) Append(ctx context.Context, frame input.Frame) error {
	select {
	case s.frames <- frame:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pump collects text until the client closes it or the stream pauses, then
// either replays cached audio or synthesizes. It never returns without the
// caller closing both channels.
func (s *playbackReuseSession) pump() {
	accepted := make([]input.Frame, 0, 8)
	var text []byte
	var count, offset uint64
	var broken bool

	idle := time.NewTimer(playbackReuseIdle)
	defer idle.Stop()
collect:
	for {
		select {
		case frame := <-s.frames:
			if frame.Sequence != count || frame.Offset != offset || len(text)+len(frame.Text) > playbackReuseLimit {
				broken = true
				break collect
			}
			accepted = append(accepted, frame)
			text = append(text, frame.Text...)
			count++
			offset += uint64(len([]rune(frame.Text)))
			if frame.Final {
				break collect
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(playbackReuseIdle)
		case <-idle.C:
			// Input paused: this is a live reply, not a settled replay.
			break collect
		case <-s.ctx.Done():
			broken = true
			break collect
		}
	}
	if !broken && len(accepted) > 0 && accepted[len(accepted)-1].Final {
		if match, ok := s.key.Match(string(text), s.cached); ok && len(match.Audio) > 0 {
			s.chunks <- match.Audio
			return
		}
	}
	if s.ctx.Err() != nil {
		s.errs <- s.ctx.Err()
		return
	}
	s.synthesize(accepted)
}

// synthesize opens the provider, replays the accepted frames, and forwards its
// audio. Later frames arrive through Append and go straight to the provider.
func (s *playbackReuseSession) synthesize(accepted []input.Frame) {
	provider, _, err := s.open(s.ctx)
	if err != nil {
		s.errs <- err
		return
	}
	defer provider.Cancel()
	for _, frame := range accepted {
		if err = provider.Append(s.ctx, frame); err != nil {
			s.errs <- err
			return
		}
		s.recorder.record(frame)
	}
	audio := provider.Audio()
	for {
		select {
		case <-s.ctx.Done():
			s.errs <- s.ctx.Err()
			return
		case frame := <-s.frames:
			if err = provider.Append(s.ctx, frame); err != nil {
				s.errs <- err
				return
			}
			s.recorder.record(frame)
		case chunk, open := <-audio.Stream:
			if !open {
				if err := audio.Err(); err != nil {
					s.errs <- err
					return
				}
				// The request context ends with the response; register the
				// utterance on a detached one so the next replay can hit.
				writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(s.ctx), playbackReuseWriteTimeout)
				s.recorder.commit(writeCtx, audio.ContentType)
				cancelWrite()
				return
			}
			if len(chunk) == 0 {
				continue
			}
			s.recorder.recordAudio(chunk)
			select {
			case s.chunks <- chunk:
			case <-s.ctx.Done():
				s.errs <- s.ctx.Err()
				return
			}
		}
	}
}
