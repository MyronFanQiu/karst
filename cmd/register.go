package cmd

import (
	"fmt"
	"karst/chain"
	"karst/config"
	"karst/logger"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type registerReturnMesssage struct {
	Info   string `json:"info"`
	Status int    `json:"status"`
}

func init() {
	registerWsCmd.ConnectCmdAndWs()
	rootCmd.AddCommand(registerWsCmd.Cmd)
}

var registerWsCmd = &wsCmd{
	Cmd: &cobra.Command{
		Use:   "register [karst_address] [storage_price]",
		Short: "Register to chain as provider (for provider)",
		Long:  "Check your qualification, register karst address to chain.",
		Args:  cobra.MinimumNArgs(2),
	},
	Connecter: func(cmd *cobra.Command, args []string) (map[string]string, error) {
		reqBody := map[string]string{
			"karst_address": args[0],
			"storage_price": args[1],
		}

		return reqBody, nil
	},
	WsEndpoint: "register",
	WsRunner: func(args map[string]string, wsc *wsCmd) interface{} {
		// Base class
		timeStart := time.Now()
		logger.Debug("Register input is %s", args)

		// Check input
		karstAddr := args["karst_address"]
		if karstAddr == "" {
			errString := "The field 'karst_address' is needed"
			logger.Error(errString)
			return registerReturnMesssage{
				Info:   errString,
				Status: 400,
			}
		}

		storagePrice, err := strconv.ParseUint(args["storage_price"], 10, 64)
		if err != nil {
			errString := err.Error()
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		if storagePrice < 40 {
			errString := "The 'storage_price' must be greater than or equal to 40"
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		// Register karst address
		registerReturnMsg := RegisterToChain(karstAddr, storagePrice, wsc.Cfg)
		if registerReturnMsg.Status != 200 {
			logger.Error("Register to crust failed, error is: %s", registerReturnMsg.Info)
			return registerReturnMsg
		} else {
			registerReturnMsg.Info = fmt.Sprintf("Register '%s' successfully in %s ! You can check it on crust.", karstAddr, time.Since(timeStart))
			return registerReturnMsg
		}
	},
}

func RegisterToChain(karstAddr string, storagePrice uint64, cfg *config.Configuration) registerReturnMesssage {
	if err := chain.Register(cfg, karstAddr, storagePrice); err != nil {
		return registerReturnMesssage{
			Info:   fmt.Sprintf("Register failed, please make sure:1. Your `backup`, `password` is correct; 2. You have report works; 3. You have enough mortgage, err is: %s", err.Error()),
			Status: 400,
		}
	}

	return registerReturnMesssage{
		Status: 200,
	}
}
