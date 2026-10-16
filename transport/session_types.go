package transport

import (
	"errors"

	"github.com/p1neappleXpress/OpenFlux/transport/control"
)

// MaxNegotiatedPacket leaves space for the envelope and AES overhead in a
// 65535-byte record.
const MaxNegotiatedPacket = 65000

// ErrNegotiationPending is returned by Session.Send / SendControl before the
// handshake has completed.
var ErrNegotiationPending = errors.New("authenticated peer negotiation is not complete")

// PeerParameters carries the peer's advertised capabilities.
type PeerParameters struct {
	Capabilities  control.Capabilities
	MaxPacketSize int
}

// PeerParameterProvider is implemented by Session and exposes the negotiated
// peer parameters to upper layers (e.g. gVisor link MTU).
type PeerParameterProvider interface {
	PeerParameters() (PeerParameters, bool)
}

// ControlHandler receives decoded control packets from the peer. It runs in
// its own goroutine, so it may block without stalling the receive loop.
type ControlHandler func(subtype control.Subtype, payload []byte)

// SendFill is Send for a packet that is copied straight into the buffer the
// session builds (see FillSender).
func (s *Session) SendFill(size int, fill func(dst []byte)) error {
	raw := getBuf(control.EnvelopeSize + size)
	fill(raw[control.EnvelopeSize:])
	return s.send(raw[control.EnvelopeSize:], raw)
}

// CarrierMTU is the IP packet size the tunnel stack on top of this session
// can use: the smallest of what its carriers declare (DefaultCarrierMTU for
// one that says nothing) and of the packet limit this side accepts. It does
// not depend on which carriers are connected right now, so a failover does not
// strand packets that are already larger than the carrier that takes over.
func (s *Session) CarrierMTU() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.links) == 0 {
		return DefaultCarrierMTU
	}
	mtu := s.params.MaxPacketSize
	for _, l := range s.links {
		v := DefaultCarrierMTU
		if c, ok := l.raw.(CarrierMTUer); ok {
			v = c.CarrierMTU()
		}
		mtu = min(mtu, v)
	}
	return max(mtu, 1280)
}
