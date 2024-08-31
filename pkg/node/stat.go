package node

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/fuse"
	"github.com/hanwen/go-fuse/v2/fs"
)

// Ensure that we implement NodeStatfser
var _ = (fs.NodeStatfser)((*CryptNode)(nil))

// Statfs implements the fs.NodeStatfser interface
func (node *CryptNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	// TODO: Not implemented (yet_
	return 0
}
