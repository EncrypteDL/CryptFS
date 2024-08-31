package main

import (
	"errors"
	"os"
	"os/signal"

	"github.com/EncrypteDL/CryptFS/pkg/network/client"
	"github.com/EncrypteDL/CryptFS/pkg/node"
	"github.com/EncrypteDL/CryptFS/pkg/storage"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// mountCmd represents the mount command
var mountCmd = &cobra.Command{
	Use:     "mount [flags] <metadataserver> <blobserver> <mountpoint>",
	Aliases: []string{""},
	Short:   "Mounts a DinoFS file system",
	Long:    `...`,
	Args:    cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		debug := viper.GetBool("debug")
		cache := viper.GetString("cache")

		metadataStore := args[0]
		blobServer := args[1]
		mountPoint := args[2]

		mount(debug, cache, metadataStore, blobServer, mountPoint)
	},
}

func init() {
	RootCmd.AddCommand(mountCmd)

	mountCmd.Flags().StringP(
		"cache", "c", "./cache",
		"Set the directory used to store cache blobs",
	)

	viper.BindPFlag("cache", mountCmd.Flags().Lookup("cache"))
	viper.SetDefault("cache", "./cache")
}

func mount(debug bool, cache, metadataServer, blobServer, mountPoint string) {
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		log.WithError(err).Fatal("error creating mount point")
	}

	var factory node.CryptNodeFactory

	metadataStore := storage.NewRemoteVersionedStore(
		client.New(
			client.WithAddress(metadataServer),
			client.WithFallbackToPlainTCP(),
		),
		storage.WithChangeListener(factory.InvalidateCache),
	)
	metadataStore.Start()
	defer metadataStore.Stop()

	remoteStore := storage.NewRemoteStore(blobServer)
	pairedStore := storage.NewPaired(
		storage.NewDiskStore(os.ExpandEnv(cache)),
		remoteStore,
	)
	blogStore := storage.NewBlobStore(pairedStore)

	factory.Blobs = blogStore
	factory.Metadata = metadataStore

	g := node.NewInodeNumbersGenerator()
	go g.Start()
	defer g.Stop()
	factory.InodeGenerator = g

	var fsopts fs.Options
	fsopts.Debug = debug
	fsopts.UID = uint32(os.Getuid())
	fsopts.GID = uint32(os.Getgid())
	fsopts.FsName = "test" // TOOD: Where should this come from?
	fsopts.Name = "dinofs"
	var rootKey [node.NodeKeyLen]byte
	root := factory.ExistingNode("root", rootKey)
	factory.Root = root
	if err := root.LoadMetadata(root.Key); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			log.Infof("Serving an empty file system (no metadata found for root node)")
			root.Mode |= fuse.S_IFDIR
			root.Children = make(map[string]*node.DinoNode)
		} else {
			log.Fatalf("Could not load root node metadata: %v", err)
		}
	}

	mount := os.ExpandEnv(mountPoint)
	server, err := fs.Mount(mount, root, &fsopts)
	if err != nil {
		log.Fatalf("Could not mount on %q: %v", mount, err)
	}

	// Before we call srv.Serve(), which never returns unless srv.Shutdown() is
	// called, we need to install a signal handler to call srv.Shutdown().
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		sig := <-c
		log.WithField("signal", sig).Info("Shutting down fuse server")
		if err := server.Unmount(); err != nil {
			log.WithFields(log.Fields{"err": err}).Warn("Could not unmount filesystem")
		}
	}()

	server.Wait()
}
