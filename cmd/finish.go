package cmd

import (
	"encoding/json"
	"fmt"
	"karst/chain"
	"karst/config"
	"karst/logger"
	"karst/merkletree"
	"karst/model"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

type finishReturnMessage struct {
	Info   string `json:"info"`
	Status int    `json:"status"`
}

func init() {
	finishWsCmd.ConnectCmdAndWs()
	rootCmd.AddCommand(finishWsCmd.Cmd)
}

var finishWsCmd = &wsCmd{
	Cmd: &cobra.Command{
		Use:   "finish [merkle_tree] [merchant]",
		Short: "Notify the merchant that the file has been transferred",
		Long:  "Notify the merchant that the file has been transferred, the merchant will deal this file",
		Args:  cobra.MinimumNArgs(2),
	},
	Connecter: func(cmd *cobra.Command, args []string) (map[string]string, error) {
		reqBody := map[string]string{
			"merkle_tree": args[0],
			"merchant":    args[1],
		}
		return reqBody, nil
	},
	WsEndpoint: "finish",
	WsRunner: func(args map[string]string, wsc *wsCmd) interface{} {
		// Base class
		timeStart := time.Now()
		logger.Debug("Finish input is %s", args)

		// Check input
		merkleTree := args["merkle_tree"]
		if merkleTree == "" {
			errString := "The field 'merkle_tree' is needed"
			logger.Error(errString)
			return finishReturnMessage{
				Info:   errString,
				Status: 400,
			}
		}

		var mt merkletree.MerkleTreeNode
		err := json.Unmarshal([]byte(merkleTree), &mt)
		if err != nil || !mt.IsLegal() {
			errString := fmt.Sprintf("The field 'merkle_tree' is illegal, err is: %s", err)
			logger.Error(errString)
			return finishReturnMessage{
				Info:   errString,
				Status: 400,
			}
		}

		merchant := args["merchant"]
		if merchant == "" {
			errString := "The field 'merchant' is needed"
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		// Notify merchant to finish this file
		finishReturnMsg := notifyMerchantFinish(&mt, merchant, wsc.Cfg)
		if finishReturnMsg.Status != 200 {
			logger.Error("Request merchant '%s' to finish '%s' failed, error is: %s", mt.Hash, merchant, finishReturnMsg.Info)
			return finishReturnMsg
		} else {
			finishReturnMsg.Info = fmt.Sprintf("Request merchant '%s' to finish '%s' successfully in %s !", mt.Hash, merchant, time.Since(timeStart))
			return finishReturnMsg
		}
	},
}

func notifyMerchantFinish(mt *merkletree.MerkleTreeNode, merchant string, cfg *config.Configuration) finishReturnMessage {
	// Get merchant unseal address
	karstBaseAddr, err := chain.GetMerchantAddr(cfg, merchant)
	if err != nil {
		return finishReturnMessage{
			Info:   fmt.Sprintf("Can't read karst address of '%s', error: %s", merchant, err),
			Status: 400,
		}
	}

	karstFileFinishAddr := karstBaseAddr + "/api/v0/file/finish"
	logger.Debug("Get file finish address '%s' of '%s' success.", karstFileFinishAddr, merchant)

	// Request merchant to seal file and give store proof
	logger.Info("Connecting to %s to finish file", karstFileFinishAddr)
	c, _, err := websocket.DefaultDialer.Dial(karstFileFinishAddr, nil)
	if err != nil {
		return finishReturnMessage{
			Info:   err.Error(),
			Status: 500,
		}
	}
	defer c.Close()

	fileFinishMsg := model.FileFinishMessage{
		Client:     cfg.Crust.Address,
		MerkleTree: mt,
	}

	fileFinishMsgBytes, err := json.Marshal(fileFinishMsg)
	if err != nil {
		return finishReturnMessage{
			Info:   err.Error(),
			Status: 500,
		}
	}

	logger.Debug("File finish message is: %s", string(fileFinishMsgBytes))

	if err = c.WriteMessage(websocket.TextMessage, fileFinishMsgBytes); err != nil {
		return finishReturnMessage{
			Info:   err.Error(),
			Status: 500,
		}
	}

	_, message, err := c.ReadMessage()
	if err != nil {
		return finishReturnMessage{
			Info:   err.Error(),
			Status: 500,
		}
	}
	logger.Debug("File finish return: %s", message)

	fileFinishReturnMsg := model.FileFinishReturnMessage{}
	if err = json.Unmarshal(message, &fileFinishReturnMsg); err != nil {
		return finishReturnMessage{
			Info:   err.Error(),
			Status: 500,
		}
	}

	return finishReturnMessage{
		Info:   fileFinishReturnMsg.Info,
		Status: fileFinishReturnMsg.Status,
	}
}
