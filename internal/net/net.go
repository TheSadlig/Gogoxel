// Package net is the foundation of multiplayer networking. See issue
// #27. The first slice defines the transport-agnostic message envelope
// and a snapshot-vs-input split so the gameplay layer can be written
// against an interface rather than a concrete protocol.
package net

// SeqNum is a monotonically-increasing per-channel sequence id.
type SeqNum uint32

// Kind is the message discriminator.
type Kind uint8

const (
	KindInput    Kind = 1
	KindSnapshot Kind = 2
	KindRPC      Kind = 3
	KindAck      Kind = 4
)

// Message is the envelope written over the wire.
type Message struct {
	Kind    Kind
	Seq     SeqNum
	Payload []byte
}

// Transport is the minimal interface a backend (QUIC, ENet, raw UDP)
// must satisfy.
type Transport interface {
	Send(peer uint32, m Message) error
	Recv() (peer uint32, m Message, err error)
}
