package node

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	sync "github.com/sasha-s/go-deadlock"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// NodeKeyLen is the default node key length
	NodeKeyLen int = 20

	modeNotLoaded uint32 = 0xffffffff
)

// CryptNode holds information about a filesystem node and implements the FUSE filesystem interface
type CryptNode struct {
	fs.Inode

	// Injected by the node factory itself.
	factory *CryptNodeFactory

	mu sync.Mutex

	shouldSaveMetadata   bool
	shouldReloadMetadata bool
	shouldSaveContent    bool

	User  uint32
	Group uint32
	Mode  uint32
	Time  time.Time

	// Not persisted, only for logging
	name string

	// Used as the Key to save/retrieve this node in the metadata store. It's a
	// sort of inode number, but it's not assigned by a central entity and can't
	// be reused.
	Key [NodeKeyLen]byte

	// Increases by one at each update (by any client connected to the metadata
	// server).
	version uint64

	xattrs map[string][]byte

	// Only makes sense for regular files or symlinks:
	contentKey []byte
	content    []byte

	// Only makes sense for directories:
	Children map[string]*CryptNode
}

// Setxattr ...
func (node *CryptNode) Setxattr(ctx context.Context, attr string, data []byte, flags uint32) syscall.Errno {
	// Implementing this method seems to be needed to compile plan9port in dinofs.
	// This is required when executing "install o.mk /n/dino/src/plan9port/bin/mk".
	// Wrapping that with strace shows:
	//
	//	fsetxattr(…) = -1 ENODATA (No data available)
	//
	// After adding this, compilation fails later on with some segmentation fault
	// which I need to investigate at some point, but that use case might be out of
	// scope for this project, at least for now.
	//
	// According to setxattr(2):
	//
	// By default (i.e., flags is zero), the extended attribute will be created if
	// it does not exist, or the value will be replaced if the attribute already
	// exists. To modify these semantics, one of the following values can be
	// specified in flags:
	//
	// XATTR_CREATE Perform a pure create, which fails if the named attribute
	// exists already.
	//
	// XATTR_REPLACE Perform a pure replace operation, which fails if the named
	// attribute does not already exist.
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.xattrs == nil {
		node.xattrs = make(map[string][]byte)
	}
	switch flags {
	case unix.XATTR_CREATE:
		if _, ok := node.xattrs[attr]; ok {
			return syscall.EEXIST
		}
	case unix.XATTR_REPLACE:
		if _, ok := node.xattrs[attr]; !ok {
			return syscall.ENODATA
		}
	}
	rbdata := node.xattrs[attr]
	node.xattrs[attr] = append([]byte{}, data...)
	node.shouldSaveMetadata = true
	errno := node.sync()
	// Rollback.
	if errno != 0 {
		if rbdata != nil {
			node.xattrs[attr] = rbdata
		} else {
			delete(node.xattrs, attr)
		}
	}
	return errno
}

// Getxattr should read data for the given attribute into
// `dest` and return the number of bytes. If `dest` is too
// small, it should return ERANGE and the size of the attribute.
// If not defined, Getxattr will return ENOATTR.
func (node *CryptNode) Getxattr(ctx context.Context, attr string, dest []byte) (uint32, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.xattrs == nil {
		return 0, syscall.ENODATA
	}
	value, ok := node.xattrs[attr]
	if !ok {
		return 0, syscall.ENODATA
	}
	if len(value) > len(dest) {
		return uint32(len(value)), syscall.ERANGE
	}
	return uint32(copy(dest, value)), 0
}

// Rmdir ...
func (node *CryptNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()
	child := node.Children[name]
	// go-fuse should know to call into Rmdir only if the child exists.
	// Since a panic() here would break the mount, let's be defensive anyway.
	if child == nil {
		log.WithFields(log.Fields{
			"name": name,
		}).Warn("Asked to remove directory that does not exist")
		return syscall.ENOENT
	}
	child.mu.Lock()
	defer child.mu.Unlock()
	if len(child.Children) != 0 {
		return syscall.ENOTEMPTY
	}
	delete(node.Children, name)
	node.shouldSaveMetadata = true
	errno := node.sync()
	// Rollback.
	if errno != 0 {
		node.Children[name] = child
	}
	return errno
}

// Unlink ...
func (node *CryptNode) Unlink(ctx context.Context, name string) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()
	child := node.Children[name]
	delete(node.Children, name)
	node.shouldSaveMetadata = true
	errno := node.sync()
	// Rollback.
	if errno != 0 && child != nil {
		node.Children[name] = child
	}
	return errno
}

// Call with lock held.
func (node *CryptNode) fullPath() string {
	return node.Path(node.factory.Root.EmbeddedInode())
}

// Call with lock held.
func (node *CryptNode) reloadIfNeeded() syscall.Errno {
	if !node.shouldReloadMetadata {
		return 0
	}
	logger := log.WithField("parent", node.name)
	nn := &CryptNode{factory: node.factory}
	if err := nn.LoadMetadata(node.Key); err != nil {
		logger.WithField("err", err).Error("Could not reload")
		return syscall.EIO
	}
	node.shouldSaveMetadata = false
	node.shouldReloadMetadata = false
	node.shouldSaveContent = false
	node.User = nn.User
	node.Group = nn.Group
	node.Mode = nn.Mode
	node.Time = nn.Time
	if node.version != nn.version {
		logger.Debugf("Version changed from %d to %d", node.version, nn.version)
		node.version = nn.version
	}
	node.xattrs = nn.xattrs
	if !bytes.Equal(node.contentKey, nn.contentKey) {
		logger.Debug("Content changed, marking for lazy reload")
		node.contentKey = nn.contentKey
		node.content = nil
	}

	// Children are by far the hardest part to reload. I've spent way too many
	// hours trying to make this work.

	for name, child := range nn.Children {
		logger := logger.WithField("name", name)
		if prev := node.Children[name]; prev != nil {
			if prev.Key == child.Key {
				logger.Debug("Child kept same key - no op")
			} else {
				logger.Debug("Child changed key - updating that and marking for reload")
				prev.Key = child.Key
				prev.shouldReloadMetadata = true
			}
		} else {
			logger.Debug("Child is new, adding for lazy loading")
			child.name = name
			node.Children[name] = child
		}
	}

	for name := range node.Children {
		if nn.Children[name] == nil {
			logger.Debug("Child has been removed, removing here too")
			node.RmChild(name)
			delete(node.Children, name)
		}
	}

	return 0
}

// Opendir ...
func (node *CryptNode) Opendir(ctx context.Context) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()
	if errno := node.reloadIfNeeded(); errno != 0 {
		return errno
	}
	for _, childNode := range node.Children {
		if errno := node.ensureChildLoaded(ctx, childNode); errno != 0 {
			return errno
		}
	}
	return 0
}

// Lookup ...
func (node *CryptNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if errno := node.reloadIfNeeded(); errno != 0 {
		return nil, errno
	}
	child := node.Children[name]
	if child == nil {
		return nil, syscall.ENOENT
	}
	if errno := node.ensureChildLoaded(ctx, child); errno != 0 {
		return nil, errno
	}

	// TODO Should persist the content size in the metadata instead of having to
	// load the contents just for lookup!
	//
	// In the below, if we don't report the size, any read to a mmap-ed file
	// whose *dinoNode content hasn't been loaded would cause a SIGBUS.
	// We wouldn't even get i/o calls to the *dinoNode.
	if errno := child.ensureContentLoaded(); errno != 0 {
		return nil, errno
	}
	out.Uid = child.User
	out.Gid = child.Group
	out.Mode = child.Mode
	out.Atime = uint64(child.Time.Unix())
	out.Mtime = uint64(child.Time.Unix())
	out.Size = uint64(len(child.content))

	return child.EmbeddedInode(), 0
}

// Call with lock held.
func (node *CryptNode) ensureChildLoaded(ctx context.Context, childNode *CryptNode) syscall.Errno {
	if childNode.Mode != modeNotLoaded {
		return 0
	}
	if err := childNode.LoadMetadata(childNode.Key); err != nil {
		log.WithFields(log.Fields{
			"err":    err,
			"child":  childNode.name,
			"parent": node.fullPath(),
		}).Error("could not load metadata")
		return syscall.EIO
	}
	node.AddChild(childNode.name, node.NewInode(ctx, childNode, fs.StableAttr{
		Mode: childNode.Mode,
		Ino:  node.factory.InodeGenerator.Next(),
	}), false)
	return 0
}

// Flush ...
func (node *CryptNode) Flush(ctx context.Context, f fs.FileHandle) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()
	prev := node.contentKey
	errno := node.sync()
	if errno != 0 {
		fmt.Printf("%#v\n", prev)
		fmt.Printf("%#v\n", node.contentKey)
	}
	if errno != 0 && !bytes.Equal(prev, node.contentKey) {
		fmt.Println("rolling back...")
		// Rollback.
		node.contentKey = prev
		node.content = nil
	}
	return errno
}

// Release would sync writes to mmap-ed files.
func (node *CryptNode) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	return node.Flush(ctx, f)
}

// Fsync ...
func (node *CryptNode) Fsync(ctx context.Context, f fs.FileHandle, flags uint32) syscall.Errno {
	return node.Flush(ctx, f)
}

// Getattr ...
func (node *CryptNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()
	if errno := node.reloadIfNeeded(); errno != 0 {
		return errno
	}
	if errno := node.ensureContentLoaded(); errno != 0 {
		return errno
	}
	out.Uid = node.User
	out.Gid = node.Group
	out.Mode = node.Mode
	out.Atime = uint64(node.Time.Unix())
	out.Mtime = uint64(node.Time.Unix())
	out.Size = uint64(len(node.content))
	return 0
}

// Create ...
func (node *CryptNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	child, rollback, errno := node.createLockedChild(ctx, name, mode, fuse.S_IFREG)
	if errno != 0 {
		return nil, nil, 0, errno
	}
	defer child.mu.Unlock()
	child.shouldSaveMetadata = true
	if errno := child.sync(); errno != 0 {
		rollback()
		return nil, nil, 0, errno
	}
	node.shouldSaveMetadata = true
	if errno := node.sync(); errno != 0 {
		rollback()
		return nil, nil, 0, errno
	}
	return child.EmbeddedInode(), nil, 0, 0
}

// Mkdir ...
func (node *CryptNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	child, rollback, errno := node.createLockedChild(ctx, name, mode, fuse.S_IFDIR)
	if errno != 0 {
		return nil, errno
	}
	defer child.mu.Unlock()
	child.Children = make(map[string]*CryptNode)
	child.shouldSaveMetadata = true
	node.shouldSaveMetadata = true
	if errno := child.sync(); errno != 0 {
		rollback()
		return nil, errno
	}
	if errno := node.sync(); errno != 0 {
		rollback()
		return nil, errno
	}
	return child.EmbeddedInode(), 0
}

// Symlink ...
func (node *CryptNode) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	child, rollback, errno := node.createLockedChild(ctx, name, 0, fuse.S_IFLNK)
	if errno != 0 {
		return nil, errno
	}
	defer child.mu.Unlock()
	child.shouldSaveContent = true
	child.content = []byte(target)
	child.shouldSaveMetadata = true
	node.shouldSaveMetadata = true
	if errno := child.sync(); errno != 0 {
		rollback()
		return nil, errno
	}
	if errno := node.sync(); errno != 0 {
		rollback()
		return nil, errno
	}
	return child.EmbeddedInode(), 0
}

func (node *CryptNode) createLockedChild(ctx context.Context, name string, mode uint32, orMode uint32) (child *CryptNode, rollback func(), errno syscall.Errno) {
	id := fs.StableAttr{
		Mode: mode | orMode,
		Ino:  node.factory.InodeGenerator.Next(),
	}
	child, err := node.factory.allocateNode()
	if err != nil {
		log.WithFields(log.Fields{
			"err":    err,
			"child":  name,
			"parent": node.fullPath(),
		}).Error("Create child")
		return nil, nil, syscall.EIO
	}
	child.name = name
	child.Mode = id.Mode
	node.Children[name] = child
	// Lock before adding to the tree. Caller will unlock.
	child.mu.Lock()
	node.AddChild(name, node.NewInode(ctx, child, id), false)
	return child, func() {
		node.RmChild(name)
		delete(node.Children, name)
	}, 0
}

// Open ...
func (node *CryptNode) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if errno := node.reloadIfNeeded(); errno != 0 {
		return nil, 0, errno
	}
	return nil, 0, node.ensureContentLoaded()
}

func (node *CryptNode) ensureContentLoaded() syscall.Errno {
	logger := log.WithFields(log.Fields{
		"name": node.name,
	})
	if node.shouldSaveContent {
		return 0
	}
	if node.Mode&fuse.S_IFREG == 0 && node.Mode&fuse.S_IFLNK == 0 {
		return 0
	}
	if len(node.contentKey) == 0 {
		return 0
	}
	if len(node.content) != 0 {
		return 0
	}
	value, err := node.factory.Blobs.Get(node.contentKey)
	if err != nil {
		logger.WithField("err", err).Error("Could not load content")
		return syscall.EIO
	}
	node.content = value
	return 0
}

func (node *CryptNode) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if off > int64(len(node.content)) {
		return fuse.ReadResultData(nil), 0
	}
	end := off + int64(len(dest))
	if end > int64(len(node.content)) {
		end = int64(len(node.content))
	}
	return fuse.ReadResultData(node.content[off:end]), 0
}

// Readlink ...
func (node *CryptNode) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	return node.content, 0
}

// Rename ...
func (node *CryptNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()

	child := node.GetChild(name).Operations().(*CryptNode)
	child.mu.Lock()
	defer child.mu.Unlock()
	child.name = newName

	newParentNode := newParent.EmbeddedInode().Operations().(*CryptNode)
	if node.Key != newParentNode.Key {
		newParentNode.mu.Lock()
		defer newParentNode.mu.Unlock()
	}
	newParentNode.Children[newName] = child
	delete(node.Children, name)

	child.shouldSaveMetadata = true
	newParentNode.shouldSaveMetadata = true
	node.shouldSaveMetadata = true
	if errno := child.sync(); errno != 0 {
		return errno
	}
	if errno := newParentNode.sync(); errno != 0 {
		return errno
	}
	if errno := node.sync(); errno != 0 {
		return errno
	}
	return 0
}

func (node *CryptNode) resize(size uint64) (previous []byte) {
	previous = node.content
	if size > uint64(cap(node.content)) {
		larger := make([]byte, size)
		copy(larger, node.content)
		node.content = larger
	} else {
		node.content = node.content[:size]
	}
	return previous
}

// Setattr ...
func (node *CryptNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	node.mu.Lock()
	defer node.mu.Unlock()

	var (
		rbtime    *time.Time
		rbuser    *uint32
		rbgroup   *uint32
		rbmode    *uint32
		rbsize    *int
		rbcontent []byte
	)

	if t, ok := in.GetMTime(); ok {
		rbtime = new(time.Time)
		*rbtime = node.Time
		node.Time = t
	}
	if uid, ok := in.GetUID(); ok {
		rbuser = new(uint32)
		*rbuser = node.User
		node.User = uid
	}
	if gid, ok := in.GetGID(); ok {
		rbgroup = new(uint32)
		*rbgroup = node.Group
		node.Group = gid
	}
	if mode, ok := in.GetMode(); ok {
		log.WithFields(log.Fields{
			"name":      node.name,
			"requested": bitsOf(mode),
			"old":       bitsOf(node.Mode),
			"new":       bitsOf(node.Mode&0xfffff000 | mode&0x00000fff),
		}).Debug("mode change")
		rbmode = new(uint32)
		*rbmode = node.Mode
		node.Mode = node.Mode&0xfffff000 | mode&0x00000fff
	}
	if size, ok := in.GetSize(); ok {
		rbsize = new(int)
		*rbsize = len(node.content)
		rbcontent = node.resize(size)
		node.Time = time.Now()
		node.shouldSaveContent = true
	}
	node.shouldSaveMetadata = true
	errno := node.sync()
	if errno != 0 {
		// Rollback.
		if rbtime != nil {
			node.Time = *rbtime
		}
		if rbuser != nil {
			node.User = *rbuser
		}
		if rbgroup != nil {
			node.Group = *rbgroup
		}
		if rbmode != nil {
			node.Mode = *rbmode
		}
		if rbsize != nil {
			node.content = rbcontent
		}
	}
	return errno
}

func bitsOf(mode uint32) string {
	return strconv.FormatUint(uint64(mode), 2)
}

func (node *CryptNode) Write(ctx context.Context, f fs.FileHandle, data []byte, off int64) (written uint32, errno syscall.Errno) {
	node.mu.Lock()
	defer node.mu.Unlock()

	sz := int64(len(data))
	if off+sz > int64(len(node.content)) {
		node.resize(uint64(off + sz))
	}
	copy(node.content[off:], data)
	node.Time = time.Now()
	if sz > 0 {
		node.shouldSaveContent = true
	}
	return uint32(sz), 0
}
