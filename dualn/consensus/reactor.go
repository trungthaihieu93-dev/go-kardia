package consensus

import (
	"github.com/kardiachain/go-kardia/lib/log"
	"github.com/kardiachain/go-kardia/lib/p2p"
	dproto "github.com/kardiachain/go-kardia/proto/kardiachain/dualnode"
)

const (
	// DualChannel is a channel for vote dualnode transaction
	DualChannel = byte(0x40)

	MaxMsgSize = 1048576 // 1MB
)

type Reactor struct {
	p2p.BaseReactor

	logger log.Logger

	router Router
}

func newReactor(router Router) *Reactor {
	r := &Reactor{
		router: router,
	}
	r.BaseReactor = *p2p.NewBaseReactor("DualReactor", r)
	return r
}

// NewReactor creates a new reactor instance.
func NewReactor(router Router) *Reactor {
	return newReactor(router)
}

// Receive implements Reactor by handling different message types.
func (r *Reactor) Receive(chID byte, src p2p.Peer, msgBytes []byte) {
	msg, err := DecodeMsg(msgBytes)
	if err != nil {
		r.logger.Error("error decoding message",
			"src", src.ID(), "chId", chID, "msg", msg, "err", err)
		return
	}

	switch msg := msg.(type) {
	case *dproto.Vote:
		r.router.AddVote(msg)
	case *dproto.Proposal:
		r.router.SendVote(msg, chID, src)
	}

}

// GetChannels implements Reactor
func (r *Reactor) GetChannels() []*p2p.ChannelDescriptor {
	return []*p2p.ChannelDescriptor{
		{
			ID:                  DualChannel,
			Priority:            5,
			SendQueueCapacity:   100,
			RecvBufferCapacity:  100 * 100,
			RecvMessageCapacity: MaxMsgSize,
		},
	}
}