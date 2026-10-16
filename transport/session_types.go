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
// can use for the connections it opens now: the largest packet any carrier that
// is live declares it moves (DefaultCarrierMTU for one that says nothing), within
// the limit this side accepts. A packet that later has to go over a carrier
// that cannot take it is refused with *PacketTooLargeError, and the stack
// answers the flow with an ICMP "fragmentation needed" (see tunnel).
func (s *Session) CarrierMTU() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	mtu := 0
	for _, l := range s.liveLinksLocked() {
		mtu = max(mtu, l.limit())
	}
	if mtu == 0 {
		return DefaultCarrierMTU
	}
	return max(min(mtu, s.params.MaxPacketSize), 1280)
}

// limit is the largest packet the carrier of l is known to move.
func (l *transportLink) limit() int {
	if l.mtu > 0 {
		return l.mtu
	}
	return DefaultCarrierMTU
}

// hasBigLinkLocked reports whether any carrier of the session declared a packet
// size above DefaultCarrierMTU. Only then are the limits of the carriers
// enforced: a session of carriers that say nothing keeps sending whatever the
// stack above it produces, as it always did. Caller holds s.mu.
func (s *Session) hasBigLinkLocked() bool {
	for _, l := range s.links {
		if l.mtu > DefaultCarrierMTU {
			return true
		}
	}
	return false
}

// fitLink picks a carrier for a packet of size n above DefaultCarrierMTU: chosen
// if it can take it, else the first live carrier that can, else the packet is
// refused with the largest size any live carrier moves.
func fitLink(live []*transportLink, chosen *transportLink, n int) (*transportLink, error) {
	if chosen.limit() >= n {
		return chosen, nil
	}
	best := 0
	for _, l := range live {
		if l.limit() >= n {
			return l, nil
		}
		best = max(best, l.limit())
	}
	if best == 0 {
		best = DefaultCarrierMTU
	}
	return nil, &PacketTooLargeError{Limit: best}
}
