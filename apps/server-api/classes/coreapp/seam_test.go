package coreapp

import (
	"context"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The seam package relay forwards a channel over, written out here so that the
// COMPILER says whether it is complete. It is the mirror image of the copy
// task 17 keeps of this package's Relay interface: neither side imports the
// other, and a lint over names would hold neither.
//
// It lives in a test file because nothing of this package calls it; its whole
// job is to fall over at build time if browser.Session or controllers.Tunnel
// ever lose a method the relay needs.
//
// Four names differ from the list task 17 asked for, and each for a reason
// that is written where the method is:
//
//   - GrantRaw -> SendGrant(ctx, c, credit, handshake). A second exported
//     function that builds a GRANT would break the one sentence browser holds
//     with a lint AND with the unexported link: "only SendGrant builds a GRANT,
//     and it asks Allow first". So SendGrant grew the handshake instead.
//   - Data -> SendData, EOF -> SendEOF, to read like the SendGrant beside them.
//   - Hop.Table() *link.Table -> Open(window) and Get(num). *link.Table carries
//     Deliver, which is what PUTS a frame into a channel's queue: a relay that
//     held the table could hand itself frames the controller never sent, and
//     that is exactly the sentence the whole forwarding rests on. Open and Get
//     are everything the relay did with the table.
//   - Hop.Write(f wire.Frame) -> SendOpen / SendData / SendEOF / SendClose /
//     Grant. This is the one change that buys something the compiler holds:
//     with a general frame writer, a relay could put a GRANT on a tunnel
//     without raising what may arrive, or DATA without spending the credit.
//     With typed methods there is no way to build either frame except the one
//     that counts first.
type shellSeam interface {
	SendGrant(ctx context.Context, c *link.Channel, credit uint32, handshake []byte) error
	SendData(ctx context.Context, c *link.Channel, payload []byte) error
	SendEOF(ctx context.Context, c *link.Channel) error
	CloseWith(ctx context.Context, c *link.Channel, cl wire.Close) error
	Close(ctx context.Context, code wire.Code, num uint32) error
	Next(ctx context.Context, c *link.Channel) (wire.Frame, bool)
}

type hopSeam interface {
	Open(window uint32) (*link.Channel, error)
	Get(num uint32) (*link.Channel, bool)
	SendOpen(ctx context.Context, c *link.Channel, payload []byte) error
	SendData(ctx context.Context, c *link.Channel, payload []byte) error
	SendEOF(ctx context.Context, c *link.Channel) error
	SendClose(ctx context.Context, c *link.Channel, cl wire.Close) error
	Grant(ctx context.Context, c *link.Channel, credit uint32) error
	Next(ctx context.Context, c *link.Channel) (wire.Frame, bool)
}

var (
	_ shellSeam = (*browser.Session)(nil)
	_ hopSeam   = (*controllers.Tunnel)(nil)
)
