package transferclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

var ErrAssociationFailed = errors.New("delivery verification failed or exhausted")

// LocalPublication is supplied by the existing destination storage owner. It
// must reauthorize the task and destination, bind the whole immutable reference,
// and persist a single winner between Publish and Resolve(cancel=true). A
// committed outcome means durable local publication; canceled means a durable
// fence against every delayed Publish. Absence alone cannot confirm cancellation.
// Unknown OS publication outcomes must remain pending and be reconciled by file
// identity/hash. A NAS response cannot implement this interface.
type LocalPublication interface {
	Publish(context.Context, Reference, io.Reader) (AssociationOutcome, error)
	Resolve(context.Context, Reference, bool) (AssociationOutcome, error)
}

// DeliveryBinding connects the existing durable queue to the candidate operation
// handler using this client's approved HTTPS origin and per-request identity.
// No task token or local destination store is created here. A nil publication
// port is usable for uploads only; downloads fail closed.
func (c *Client) DeliveryBinding(publication LocalPublication) *AssociationBinding {
	return &AssociationBinding{
		Commit: func(ctx context.Context, direction string, ref Reference, content io.Reader) error {
			local := ""
			if direction == "download" {
				if publication == nil {
					return ErrBinding
				}
				state, err := publication.Resolve(ctx, ref, false)
				if err != nil {
					return err
				}
				switch state {
				case OutcomeCommitted:
				case OutcomePending:
					return ErrTransport
				case OutcomeCanceled:
					return ErrAssociationFailed
				case OutcomeAbsent:
					// Recheck current server authorization before entering the host's
					// atomic local publication boundary. It reauthorizes locally too.
					remote, err := c.delivery(ctx, direction, ref, false, false, "")
					if err != nil {
						return err
					}
					if remote != OutcomeAbsent {
						return ErrProtocol
					}
					if content == nil {
						return ErrBinding
					}
					state, err = publication.Publish(ctx, ref, content)
					if err != nil {
						return err
					}
					if state == OutcomePending {
						return ErrTransport
					}
					if state != OutcomeCommitted {
						return ErrAssociationFailed
					}
				default:
					return ErrProtocol
				}
				local = "published"
			}
			out, err := c.delivery(ctx, direction, ref, true, false, local)
			if err != nil {
				return err
			}
			if out == OutcomePending {
				return ErrTransport
			}
			if out != OutcomeCommitted {
				return ErrProtocol
			}
			return nil
		},
		Resolve: func(ctx context.Context, direction string, ref Reference, cancel bool) (AssociationOutcome, error) {
			if direction == "upload" {
				return c.delivery(ctx, direction, ref, false, cancel, "")
			}
			if direction != "download" || publication == nil {
				return "", ErrBinding
			}
			local, err := publication.Resolve(ctx, ref, cancel)
			if err != nil {
				return "", err
			}
			switch local {
			case OutcomePending:
				return OutcomePending, nil
			case OutcomeCommitted:
				// Cancellation cannot retract the local winner. Ensure its NAS
				// association is submitted even if the original request never arrived.
				return c.delivery(ctx, direction, ref, true, false, "published")
			case OutcomeCanceled:
				return c.delivery(ctx, direction, ref, false, true, "fenced")
			case OutcomeAbsent:
				if cancel {
					return OutcomePending, nil
				} // No durable local fence.
				out, err := c.delivery(ctx, direction, ref, false, false, "")
				if err == nil && out != OutcomeAbsent {
					return "", ErrProtocol
				}
				return out, err
			default:
				return "", ErrProtocol
			}
		},
	}
}

func (c *Client) delivery(ctx context.Context, direction string, ref Reference, commit, cancel bool, publication string) (AssociationOutcome, error) {
	if c == nil || !validID(ref.OperationID) || !validID(ref.DeliveryID) || !validID(ref.BotID) || !validHash(ref.ContentHash) || ref.SizeBytes <= 0 || (direction != "upload" && direction != "download") {
		return "", ErrInvalid
	}
	body, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		Direction   string `json:"direction"`
		ContentHash string `json:"content_hash"`
		SizeBytes   int64  `json:"size_bytes"`
		Mime        string `json:"mime"`
		Publication string `json:"publication,omitempty"`
		Cancel      bool   `json:"cancel,omitempty"`
	}{1, direction, ref.ContentHash, ref.SizeBytes, ref.Mime, publication, cancel})
	action := "resolve"
	if commit {
		action = "commit"
	}
	var out struct {
		Version  int                `json:"version"`
		Outcome  AssociationOutcome `json:"outcome"`
		Revision string             `json:"revision"`
	}
	err := c.json(ctx, http.MethodPost, "/internal/computer-deliveries/"+ref.OperationID+"/"+ref.DeliveryID+"/"+action, body, http.Header{"Content-Type": {"application/json"}}, http.StatusOK, &out)
	if err != nil {
		return "", err
	}
	if _, err := strconv.ParseUint(out.Revision, 10, 63); err != nil || out.Version != 2 {
		return "", ErrProtocol
	}
	switch out.Outcome {
	case OutcomeCommitted, OutcomeAbsent, OutcomeCanceled, OutcomePending:
		return out.Outcome, nil
	case "failed":
		return "", ErrAssociationFailed
	default:
		return "", ErrProtocol
	}
}
