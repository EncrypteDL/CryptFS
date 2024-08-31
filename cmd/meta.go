package main

import (
	"os"
	"os/signal"

	"github.com/EncrypteDL/CryptFS/pkg/network/server"
	"github.com/EncrypteDL/CryptFS/pkg/storage"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// metaServer represents the blobserver command
var metaServer = &cobra.Command{
	Use:     "meta [flags]",
	Aliases: []string{"metadataserver"},
	Short:   "Starts a metadata server",
	Long:    `...`,
	Args:    cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		bindAddress := viper.GetString("meta-bind")
		storeURI := viper.GetString("store")

		metaserver(bindAddress, storeURI)
	},
}

func init() {
	RootCmd.AddCommand(metaServer)

	metaServer.Flags().StringP(
		"store", "s", "bitcask://dinofs.db",
		"Set the store to use for storing metadata",
	)

	metaServer.Flags().StringP(
		"bind", "b", ":8000",
		"Set the [interface]:<port> to listen on",
	)

	viper.BindPFlag("meta-bind", metaServer.Flags().Lookup("bind"))
	viper.SetDefault("meta-bind", ":8000")

	viper.BindPFlag("store", metaServer.Flags().Lookup("store"))
	viper.SetDefault("store", "bitcask://dinofs.db")
}

func metaserver(bindAddress, storeURI string) {
	store, err := storage.NewStore(storeURI)
	if err != nil {
		log.Fatalf("Could not instantiate backend store: %v", err)
	}
	versionedStore := storage.NewVersionedWrapper(store)

	srv := server.New(
		server.WithBind(bindAddress),
		server.WithVersionedStore(versionedStore),
	)

	if _, err := srv.Listen(); err != nil {
		log.WithError(err).Fatal("error starting metdata server")
	}
	log.Infof("metadata server listening on %s", bindAddress)

	// Before we call srv.Serve(), which never returns unless srv.Shutdown() is
	// called, we need to install a signal handler to call srv.Shutdown().
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		sig := <-c
		log.WithField("signal", sig).Info("Shutting down metadata server")
		// Will make srv.Serve() return, and allow deferred clean-up functions to
		// execute.
		if err := srv.Shutdown(); err != nil {
			log.WithFields(log.Fields{"err": err}).Warn("Could not shut down the server cleanly")
		}
	}()

	if err := srv.Serve(); err != nil {
		log.Error(err)
	}
}
