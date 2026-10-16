package hosts

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// InviteTTL is how long an invite is good for.
const InviteTTL = 10 * time.Minute

// Invite is a one-time invite for one box. Nothing of it is stored: it
// lives in the admin's `hosts invite` while that command waits.
type Invite struct {
	ID      string // 8 lowercase hex characters (4 random bytes); not secret
	Secret  string // 128 random bits, base32 (std alphabet, no padding, upper case)
	Name    string // the box this invite is for
	Secure  bool
	Accept  []string
	Replace bool
	Expires time.Time
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewInvite makes an invite for name that expires InviteTTL after now.
func NewInvite(name string, now time.Time) (Invite, error) {
	id := make([]byte, 4)
	secret := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Invite{}, err
	}
	if _, err := rand.Read(secret); err != nil {
		return Invite{}, err
	}
	return Invite{
		ID:      hex.EncodeToString(id),
		Secret:  b32.EncodeToString(secret),
		Name:    name,
		Expires: now.Add(InviteTTL),
	}, nil
}

// Code is what the admin hands the new box: "<id>-<secret>".
func (i Invite) Code() string { return i.ID + "-" + i.Secret }

// ParseCode splits a code into its id and its secret, upper-casing the
// secret; a bad id or a secret that isn't 16 bytes of base32 is an error.
func ParseCode(code string) (id, secret string, err error) {
	id, secret, ok := strings.Cut(strings.TrimSpace(code), "-")
	if !ok || !validID(id) {
		return "", "", errors.New("invite code: want <8 hex characters>-<secret>")
	}
	secret = strings.ToUpper(secret)
	if _, err := secretBytes(secret); err != nil {
		return "", "", err
	}
	return id, secret, nil
}

func validID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 4 && hex.EncodeToString(b) == id
}

func secretBytes(secret string) ([]byte, error) {
	b, err := b32.DecodeString(secret)
	if err != nil || len(b) != 16 {
		return nil, errors.New("invite code: the secret is not 128 bits of base32")
	}
	return b, nil
}

// JoinSubject is the subject a join request for invite id goes to.
func JoinSubject(id string) string { return "bmhosts.join." + id }

// JoinRequest is what the new box sends, MACed under the invite's secret.
type JoinRequest struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	ApproveKey string `json:"approve_key"`
	TS         string `json:"ts"`
	Salt       string `json:"salt"`
	MAC        string `json:"mac"`
}

// JoinReply is the admin's answer: the roster and admin key, or an error;
// MACed under the invite's secret and bound to the request's salt.
type JoinReply struct {
	Roster   json.RawMessage `json:"roster"`
	AdminKey string          `json:"admin_key"`
	Error    string          `json:"error"`
	MAC      string          `json:"mac"`
}

const (
	infoRequest = "bmesh join request v1"
	infoReply   = "bmesh join reply v1"
	tagRequest  = "bmesh/join-request/v1"
	tagReply    = "bmesh/join-reply/v1"
)

// macKey is HKDF-SHA256(secret, salt=id, info), 32 bytes.
func macKey(id, secret, info string) ([]byte, error) {
	s, err := secretBytes(secret)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, s, []byte(id), info, 32)
}

func mac(key, b []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

func macOK(key, b []byte, got string) bool {
	g, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write(b)
	return hmac.Equal(m.Sum(nil), g)
}

func (r *JoinRequest) bytes() []byte {
	var e enc
	e.str(tagRequest)
	e.str(r.Name)
	e.str(r.Key)
	e.str(r.ApproveKey)
	e.str(r.TS)
	e.str(r.Salt)
	return e.b
}

func (r *JoinReply) bytes(reqSalt string) []byte {
	var e enc
	e.str(tagReply)
	e.str(reqSalt)
	e.str(r.AdminKey)
	e.str(r.Error)
	e.str(string(r.Roster))
	return e.b
}

// sealReply is rep MACed and encoded. The roster is MACed as the bytes the
// joiner will read: encoding/json compacts and escapes a RawMessage and a
// missing one arrives as null, so the reply goes through one round trip
// first.
func sealReply(key []byte, rep JoinReply, reqSalt string) ([]byte, error) {
	rep.MAC = ""
	b, err := json.Marshal(rep)
	if err != nil {
		return nil, err
	}
	var wire JoinReply
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, err
	}
	wire.MAC = mac(key, wire.bytes(reqSalt))
	return json.Marshal(wire)
}

// maxBadRequests is how many requests failing the MAC burn an invite.
const maxBadRequests = 3

// Serve answers join requests for inv on nc until the invite is burned, ctx
// ends or inv.Expires passes, and returns the request it burned on.
// A request whose MAC fails is dropped without burning; the third failure
// burns the invite (error "invite <id> burned after 3 bad requests").
// The first request whose MAC verifies burns it: then the name must equal
// inv.Name and be ValidHostName, else the reply carries the refusal; when
// it does, accept is called and its roster and admin key, or its error,
// are the reply. The reply is MACed. A refused request is returned with
// the refusal as the error. After expiry: error "invite <id> expired".
func Serve(ctx context.Context, nc *nats.Conn, inv Invite,
	accept func(JoinRequest) (roster json.RawMessage, adminKey string, err error)) (JoinRequest, error) {
	reqKey, err := macKey(inv.ID, inv.Secret, infoRequest)
	if err != nil {
		return JoinRequest{}, err
	}
	replyKey, err := macKey(inv.ID, inv.Secret, infoReply)
	if err != nil {
		return JoinRequest{}, err
	}
	sub, err := nc.SubscribeSync(JoinSubject(inv.ID))
	if err != nil {
		return JoinRequest{}, err
	}
	defer sub.Unsubscribe()
	if err := nc.Flush(); err != nil {
		return JoinRequest{}, err
	}
	wctx, cancel := context.WithDeadline(ctx, inv.Expires)
	defer cancel()
	bad := 0
	for {
		msg, err := sub.NextMsgWithContext(wctx)
		if err != nil {
			if ctx.Err() != nil {
				return JoinRequest{}, ctx.Err()
			}
			if wctx.Err() != nil {
				return JoinRequest{}, fmt.Errorf("invite %s expired", inv.ID)
			}
			return JoinRequest{}, err
		}
		var req JoinRequest
		if json.Unmarshal(msg.Data, &req) != nil || !macOK(reqKey, req.bytes(), req.MAC) {
			bad++
			if bad >= maxBadRequests {
				return JoinRequest{}, fmt.Errorf("invite %s burned after %d bad requests", inv.ID, maxBadRequests)
			}
			continue
		}
		var rep JoinReply
		var refusal error
		switch {
		case req.Name != inv.Name:
			refusal = fmt.Errorf("invite %s is for %s, not %s", inv.ID, inv.Name, req.Name)
		case !ValidHostName(req.Name):
			refusal = fmt.Errorf("%q is not a valid host name", req.Name)
		default:
			rep.Roster, rep.AdminKey, refusal = accept(req)
		}
		if refusal != nil {
			rep = JoinReply{Error: refusal.Error()}
		}
		if msg.Reply != "" {
			b, err := sealReply(replyKey, rep, req.Salt)
			if err == nil {
				err = nc.Publish(msg.Reply, b)
			}
			if err == nil {
				err = nc.Flush()
			}
			if err != nil && refusal == nil {
				return req, fmt.Errorf("invite %s: reply to %s: %w", inv.ID, req.Name, err)
			}
		}
		return req, refusal
	}
}

// Join sends req (Name, Key, ApproveKey set by the caller; Join sets TS,
// Salt and MAC) on JoinSubject(id) and returns the first reply whose MAC
// verifies, ignoring junk. A reply with Error set is returned as an error
// "refused: <error>". No verified reply before ctx ends: an error saying the
// code is wrong, used or expired.
func Join(ctx context.Context, nc *nats.Conn, code string, req JoinRequest) (JoinReply, error) {
	id, secret, err := ParseCode(code)
	if err != nil {
		return JoinReply{}, err
	}
	reqKey, err := macKey(id, secret, infoRequest)
	if err != nil {
		return JoinReply{}, err
	}
	replyKey, err := macKey(id, secret, infoReply)
	if err != nil {
		return JoinReply{}, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return JoinReply{}, err
	}
	req.TS = Stamp(time.Now())
	req.Salt = hex.EncodeToString(salt)
	req.MAC = mac(reqKey, req.bytes())
	body, err := json.Marshal(req)
	if err != nil {
		return JoinReply{}, err
	}
	inbox := nc.NewRespInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return JoinReply{}, err
	}
	defer sub.Unsubscribe()
	if err := nc.PublishRequest(JoinSubject(id), inbox, body); err != nil {
		return JoinReply{}, err
	}
	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			// No responders: nobody serves this invite any more.
			if ctx.Err() != nil || errors.Is(err, nats.ErrNoResponders) {
				return JoinReply{}, fmt.Errorf("no answer to invite %s: the code is wrong, used or expired", id)
			}
			return JoinReply{}, err
		}
		var rep JoinReply
		if json.Unmarshal(msg.Data, &rep) != nil || !macOK(replyKey, rep.bytes(req.Salt), rep.MAC) {
			continue
		}
		if rep.Error != "" {
			return rep, fmt.Errorf("refused: %s", rep.Error)
		}
		return rep, nil
	}
}
