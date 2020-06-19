package cmd

import (
	"karst/config"
	"karst/filesystem"
	"karst/logger"
	"karst/loop"
	"karst/tee"
	"karst/ws"
	"os"

	"github.com/spf13/cobra"
	"github.com/syndtr/goleveldb/leveldb"
)

func init() {
	rootCmd.AddCommand(daemonCmd)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start karst service",
	Long:  "Start karst service, it will use '$HOME/.karst' to run krast by default, set KARST_PATH to change execution space",
	Run: func(cmd *cobra.Command, args []string) {
		// Configuation
		cfg := config.GetInstance()
		cfg.Show()

		// DB
		db, err := leveldb.OpenFile(cfg.KarstPaths.DbPath, nil)
		if err != nil {
			logger.Error("Fatal error in opening leveldb: %s", err)
			os.Exit(-1)
		}
		defer db.Close()

		// Cmd apis
		var baseWsCommands = []*wsCmd{
			splitWsCmd,
			declareWsCmd,
			obtainWsCmd,
		}

		var providerWsCommands = []*wsCmd{
			registerWsCmd,
			listWsCmd,
		}

		// Sever model
		if cfg.TeeBaseUrl != "" && len(cfg.Fastdfs.TrackerAddrs) != 0 {
			// FS
			// TODO: Support mulitable file system
			fs, err := filesystem.OpenFastdfs(cfg)
			if err != nil {
				logger.Error("Fatal error in opening fastdfs: %s", err)
				os.Exit(-1)
			}
			defer fs.Close()

			// TEE
			tee, err := tee.NewTee(cfg.TeeBaseUrl, cfg.Crust.Backup)
			if err != nil {
				logger.Error("Fatal error in opening fastdfs: %s", err)
				os.Exit(-1)
			}

			// File seal loop
			loop.StartFileSealLoop(cfg, db, fs, tee)

			// Register provider cmd apis
			for _, wsCmd := range providerWsCommands {
				wsCmd.Register(db, cfg)
			}

			// Register base cmd apis
			for _, wsCmd := range baseWsCommands {
				wsCmd.Register(db, cfg)
			}

			logger.Info("--------- Provider model ------------")
			if err := ws.StartServer(cfg, fs, db, tee); err != nil {
				logger.Error("%s", err)
			}
		} else {
			// Register base cmd apis
			for _, wsCmd := range baseWsCommands {
				wsCmd.Register(db, cfg)
			}

			logger.Info("---------- Client model -------------")
			// Start websocket service
			if err := ws.StartServer(cfg, nil, db, nil); err != nil {
				logger.Error("%s", err)
			}

		}
	},
}
