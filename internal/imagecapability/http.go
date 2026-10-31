package imagecapability

import (
	"context"
	"errors"
	"io"
	"net/http"
)

const maxResponseBytes = maxImageBytes*4/3 + (1 << 20)

var errResponseLimit = errors.New("images response exceeds the limit")

// Keep the caller's timeout and transport while constraining the SDK's HTTP
// boundary. No redirects, replay body, retry wrapper or unbounded SDK read.
func imageHTTPClient(client *http.Client, observation *requestObservation) *http.Client {
	oneShot := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	oneShot.Transport = imageTransport{base: base, observation: observation}
	oneShot.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &oneShot
}

type imageTransport struct {
	base        http.RoundTripper
	observation *requestObservation
}

func (t imageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	request := req.Clone(req.Context())
	request.GetBody = nil
	t.observation.start(request)
	response, err := t.base.RoundTrip(request)
	t.observation.returned(req.Context(), response, err)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, errors.New("images request did not return a confirmed result")
	}
	if response.ContentLength > maxResponseBytes {
		_ = response.Body.Close()
		return nil, errResponseLimit
	}
	response.Body = &imageResponseBody{
		ReadCloser: response.Body, remaining: maxResponseBytes,
		ctx: req.Context(), observation: t.observation,
	}
	return response, nil
}

type imageResponseBody struct {
	io.ReadCloser
	remaining   int64
	ctx         context.Context
	observation *requestObservation
}

func (b *imageResponseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, errResponseLimit
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	if err != nil && b.ctx != nil && b.ctx.Err() != nil && b.observation != nil {
		b.observation.requestContext = contextState(b.ctx)
	}
	b.remaining -= int64(n)
	return n, err
}
