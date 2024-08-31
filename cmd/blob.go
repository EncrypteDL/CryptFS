package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/EncrypteDL/CryptFS/pkg/storage"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// blobCmd represents the blobserver command
var blobCmd = &cobra.Command{
	Use:     "blob [flags]",
	Aliases: []string{"blobserver"},
	Short:   "Starts a blob server",
	Long:    `...`,
	Args:    cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		dataPath := viper.GetString("data")
		bindAddress := viper.GetString("blob-bind")

		blobserver(bindAddress, dataPath)
	},
}

func init() {
	RootCmd.AddCommand(blobCmd)

	blobCmd.Flags().StringP(
		"data", "d", "./data",
		"Set the directory used to store data",
	)

	blobCmd.Flags().StringP(
		"bind", "b", ":9000",
		"Set the [interface]:<port> to listen on",
	)

	viper.BindPFlag("data", blobCmd.Flags().Lookup("data"))
	viper.SetDefault("data", "./data")

	viper.BindPFlag("blob-bind", blobCmd.Flags().Lookup("bind"))
	viper.SetDefault("blob-bind", ":9000")
}

func blobserver(bindAddress, dataPath string) {
	if err := os.MkdirAll(dataPath, 0700); err != nil {
		log.Fatalf("Could not ensure directory %q exists: %v", dataPath, err)
	}
	store := storage.NewDiskStore(dataPath)
	log.Infof("using DiskStore with path %s", dataPath)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var logger *log.Entry
		status, body := func() (int, []byte) {
			hkey := r.URL.Path[1:]
			key, err := hex.DecodeString(hkey)
			if err != nil {
				return http.StatusBadRequest, []byte(fmt.Sprintf("%q: not a valid path, expecting hex key only", r.URL.Path))
			}
			logger = log.WithFields(log.Fields{
				"op":  r.Method,
				"key": hkey,
			})
			switch r.Method {
			case http.MethodGet:
				value, err := store.Get(key)
				if errors.Is(err, storage.ErrNotFound) {
					logger.WithField("err", err).Debug("Not found")
					return http.StatusNotFound, nil
				}
				if err != nil {
					logger.WithField("err", err).Error()
					return http.StatusInternalServerError, []byte(fmt.Sprintf("%q: %v", hkey, err))
				}
				logger.Info("Success")
				return http.StatusOK, value
			case http.MethodPut:
				value, err := ioutil.ReadAll(r.Body)
				if err != nil {
					logger.WithField("err", err).Error()
					return http.StatusInternalServerError, []byte(fmt.Sprintf("%q: %v", hkey, err))
				}
				if err := store.Put(key, value); err != nil {
					logger.WithField("err", err).Error()
					return http.StatusInternalServerError, []byte(fmt.Sprintf("%q: %v", hkey, err))
				}
				logger.Info("Success")
				return http.StatusOK, nil
			default:
				logger.Warn("Bad request")
				return http.StatusBadRequest, []byte(fmt.Sprintf("%q: invalid method, expecting GET or PUT", r.Method))
			}
		}()
		w.WriteHeader(status)
		if body != nil {
			if _, err := w.Write(body); err != nil {
				logger.WithField("err", err).Error("Failed writing response")
			}
		}
	})

	log.Infof("blob server listening on %s", bindAddress)
	if err := http.ListenAndServe(bindAddress, nil); err != nil {
		log.WithField("err", err).Fatal("Could not listen and serve")
	}
}
