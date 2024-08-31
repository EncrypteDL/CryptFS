package node

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/EncrypteDL/CryptFS/pkg/storage"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	sync "github.com/sasha-s/go-deadlock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVersionedStore struct {
	mu   sync.Mutex
	err  error
	errs []error
}

func (s *fakeVersionedStore) Get([]byte) (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return 0, nil, err
	}
	return 0, nil, s.err
}

func (s *fakeVersionedStore) Put(uint64, []byte, []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return err
	}
	return s.err
}

func (s *fakeVersionedStore) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *fakeVersionedStore) setErrSequence(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = errs
}

func TestNodeMetadataRollback(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	rootdir, factory, cleanup := testMount(t)
	defer cleanup()

	ko := func() {
		factory.Metadata.(*fakeVersionedStore).setErr(errors.New("computer bought the farm"))
	}

	ok := func() {
		factory.Metadata.(*fakeVersionedStore).setErr(nil)
	}

	okko := func() {
		factory.Metadata.(*fakeVersionedStore).setErrSequence(nil, errors.New("does not compute"))
	}

	randomName := func() string {
		name := make([]byte, 16)
		rand.Read(name)
		return fmt.Sprintf("%x", name)
	}

	t.Run("Setxattr", func(t *testing.T) {
		t.Run("rolls back additions", func(t *testing.T) {
			node, err := factory.allocateNode()
			require.NoError(err)
			ko()
			errno := node.Setxattr(context.Background(), "key", []byte("value"), 0)
			assert.Equal(syscall.EIO, errno)
			assert.Len(node.xattrs, 0)
		})
		t.Run("rolls back updates", func(t *testing.T) {
			node, err := factory.allocateNode()
			require.NoError(err)
			ok()
			errno := node.Setxattr(context.Background(), "key", []byte("old value"), 0)
			assert.EqualValues(0, errno)
			ko()
			errno = node.Setxattr(context.Background(), "key", []byte("value"), 0)
			assert.Equal(syscall.EIO, errno)
			assert.Len(node.xattrs, 1)
			assert.EqualValues("old value", node.xattrs["key"])
		})
	})

	t.Run("Rmdir", func(t *testing.T) {
		t.Run("adds back removed child directory", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.Mkdir(p, 0755)
			require.NoError(err)
			_, err = os.Stat(p)
			require.NoError(err)
			ko()
			err = os.Remove(p)
			require.Error(err)
			_, err = os.Stat(p)
			require.NoError(err)

			// Second remove should succeed, while without rollback it would panic
			// (assuming entry from map non-nil) or return syscall.ENOENT if we're being
			// defensive enough.
			ok()
			err = os.Remove(p)
			require.NoError(err)
			_, err = os.Stat(p)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
	})

	t.Run("Unlink", func(t *testing.T) {
		t.Run("adds back removed child file", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("Peggy Sue"), 0644)
			require.NoError(err)
			ko()
			err = os.Remove(p)
			require.Error(err)

			// After remove failure, should still be able to read up the file.
			b, err := os.ReadFile(p)
			require.NoError(err)
			assert.EqualValues(b, "Peggy Sue")
		})
	})

	/* FIXME: Fix this failing test...
	t.Run("Flush", func(t *testing.T) {
		t.Run("reverts to old data if flush fails", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			if err := os.WriteFile(p, []byte("old contents"), 0644); err != nil {
				t.Fatalf("got %v, want nil", err)
			}
			f, err := os.OpenFile(p, os.O_WRONLY, 0644)
			if err != nil {
				t.Fatalf("got %v, want nil", err)
			}
			if _, err := f.Write([]byte("new contents")); err != nil {
				t.Fatalf("got %v, want nil", err)
			}
			ko()
			if err := f.Close(); err == nil {
				t.Fatalf("got nil, want non-nil")
			}
			ok()
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("got %v, want nil", err)
			}
			if !bytes.Equal(b, []byte("old contents")) {
				t.Errorf("got %q, want %q", b, "old contents")
			}
		})
	})
	*/

	t.Run("Create", func(t *testing.T) {
		t.Run("removes file just created if child sync fails", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ko()
			f, err := os.Create(p)
			require.Error(err)
			assert.Nil(f)
			ok()
			_, err = os.Stat(p)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
		t.Run("removes file just created if parent sync fails", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			okko()
			f, err := os.Create(p)
			require.Error(err)
			assert.Nil(f)
			ok()
			_, err = os.Stat(p)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
	})

	t.Run("Mkdir", func(t *testing.T) {
		t.Run("removes directory just created if child sync fails", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ko()
			err := os.Mkdir(p, 0755)
			require.Error(err)
			ok()
			_, err = os.Stat(p)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
		t.Run("removes directory just created if parent sync fails", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			okko()
			err := os.Mkdir(p, 0755)
			require.Error(err)
			ok()
			_, err = os.Stat(p)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
	})

	t.Run("Symlink", func(t *testing.T) {
		t.Run("removes symlink just created if child sync fails", func(t *testing.T) {
			oldname := filepath.Join(rootdir, randomName())
			err := os.WriteFile(oldname, []byte("content"), 0644)
			require.NoError(err)
			newname := filepath.Join(rootdir, randomName())
			ko()
			err = os.Symlink(oldname, newname)
			require.Error(err)
			ok()
			_, err = os.Stat(newname)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
		t.Run("removes symlink just created if parent sync fails", func(t *testing.T) {
			oldname := filepath.Join(rootdir, randomName())
			err := os.WriteFile(oldname, []byte("content"), 0644)
			require.NoError(err)
			newname := filepath.Join(rootdir, randomName())
			okko()
			err = os.Symlink(oldname, newname)
			require.Error(err)
			ok()
			_, err = os.Stat(newname)
			require.Error(err)
			assert.True(os.IsNotExist(err))
		})
	})

	t.Run("Rename", func(t *testing.T) {
		t.Skip("To be able to rollback renaming, we need transactions on the metadataserver.")
	})

	t.Run("Setattr", func(t *testing.T) {
		t.Run("rolls back time change", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			newTime := time.Unix(123456789, 0)
			ko()
			err = os.Chtimes(p, newTime, newTime)
			require.Error(err)
			fi, err := os.Stat(p)
			require.NoError(err)
			assert.NotEqual(newTime, fi.ModTime())
		})
		t.Run("rolls back owner change", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			ko()
			err = os.Chown(p, 42, -1)
			require.Error(err)
			fi, err := os.Stat(p)
			require.NoError(err)
			assert.NotEqual(42, fi.Sys().(*syscall.Stat_t).Uid)
		})
		t.Run("rolls back group change", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			ko()
			err = os.Chown(p, -1, 42)
			require.Error(err)
			fi, err := os.Stat(p)
			require.NoError(err)
			assert.NotEqual(42, fi.Sys().(*syscall.Stat_t).Gid)
		})
		t.Run("rolls back mode change", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			ko()
			err = os.Chmod(p, 0111)
			require.Error(err)
			fi, err := os.Stat(p)
			require.NoError(err)
			assert.NotEqual(os.FileMode(0111), fi.Mode())
		})
		t.Run("rolls back to smaller buffer", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			ko()
			err = os.Truncate(p, 3)
			require.Error(err)
			got, err := os.ReadFile(p)
			require.NoError(err)
			assert.EqualValues("anything", got)
		})
		t.Run("rolls back to larger buffer", func(t *testing.T) {
			p := filepath.Join(rootdir, randomName())
			ok()
			err := os.WriteFile(p, []byte("anything"), 0644)
			require.NoError(err)
			ko()
			err = os.Truncate(p, 42)
			require.Error(err)
			got, err := os.ReadFile(p)
			require.NoError(err)
			assert.EqualValues("anything", got)
		})
	})
}

func testMount(t *testing.T) (mountpoint string, factory *CryptNodeFactory, cleanup func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "dinofs-test-")
	if err != nil {
		t.Fatal(err)
	}

	factory = &CryptNodeFactory{}
	factory.InodeGenerator = NewInodeNumbersGenerator()
	go factory.InodeGenerator.Start()

	factory.Metadata = &fakeVersionedStore{}
	factory.Blobs = storage.NewBlobStore(storage.NewInMemoryStore())

	var zero [NodeKeyLen]byte
	root := factory.ExistingNode("root", zero)
	root.Mode |= fuse.S_IFDIR
	root.Children = make(map[string]*CryptNode)
	factory.Root = root

	server, err := fs.Mount(dir, root, &fs.Options{
		UID: uint32(os.Getuid()),
		GID: uint32(os.Getgid()),
	})
	if err != nil {
		factory.InodeGenerator.Stop()
		t.Skipf("skipping due to fuse mount errors: %s", err)
	}

	return dir, factory, func() {
		_ = server.Unmount()
		_ = os.RemoveAll(dir)
		factory.InodeGenerator.Stop()
	}
}
