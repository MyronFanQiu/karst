package cmd

import (
	"encoding/json"
	"fmt"
	"karst/chain"
	"karst/config"
	"karst/logger"
	"karst/merkletree"
	"karst/model"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

type declareReturnMsg struct {
	Info           string `json:"info"`
	StoreOrderHash string `json:"store_order_hash"`
	Status         int    `json:"status"`
}

func init() {
	declareWsCmd.ConnectCmdAndWs()
	rootCmd.AddCommand(declareWsCmd.Cmd)
}

var declareWsCmd = &wsCmd{
	Cmd: &cobra.Command{
		Use:   "declare [merkle_tree] [duration] [merchant]",
		Short: "Declare file to chain and request merchant to generate store proof",
		Long:  "Declare file to chain and request merchant to generate store proof, the 'merkle_tree' need contain store key of each file part, the 'merchant' is chain address and 'duration' is number of blocks lasting for file storage",
		Args:  cobra.MinimumNArgs(3),
	},
	Connecter: func(cmd *cobra.Command, args []string) (map[string]string, error) {
		reqBody := map[string]string{
			"merkle_tree": args[0],
			"duration":    args[1],
			"merchant":    args[2],
		}

		return reqBody, nil
	},
	WsEndpoint: "declare",
	WsRunner: func(args map[string]string, wsc *wsCmd) interface{} {
		timeStart := time.Now()
		logger.Debug("Declare input is %s", args)

		// Check input
		merkleTree := args["merkle_tree"]
		if merkleTree == "" {
			errString := "The field 'merkle_tree' is needed"
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		var mt merkletree.MerkleTreeNode
		err := json.Unmarshal([]byte(merkleTree), &mt)
		if err != nil || !mt.IsLegal() {
			errString := fmt.Sprintf("The field 'merkle_tree' is illegal, err is: %s", err)
			logger.Error(errString)
			return declareReturnMsg{
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

		duration, err := strconv.ParseUint(args["duration"], 10, 64)
		if err != nil {
			errString := err.Error()
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		if duration <= 30 {
			errString := "The duration must be greater than 300"
			logger.Error(errString)
			return declareReturnMsg{
				Info:   errString,
				Status: 400,
			}
		}

		// Declare message
		declareReturnMsg := declareFile(mt, merchant, duration, wsc.Cfg)
		if declareReturnMsg.Status != 200 {
			logger.Error(declareReturnMsg.Info)
		} else {
			declareReturnMsg.Info = fmt.Sprintf("Declare successfully in %s ! Store order hash is '%s'.", time.Since(timeStart), declareReturnMsg.StoreOrderHash)
			logger.Info(declareReturnMsg.Info)
		}

		return declareReturnMsg
	},
}

func declareFile(mt merkletree.MerkleTreeNode, merchant string, duration uint64, cfg *config.Configuration) declareReturnMsg {
	// Get merchant seal address
	karstBaseAddr, err := chain.GetMerchantAddr(cfg, merchant)
	if err != nil {
		return declareReturnMsg{
			Info:   fmt.Sprintf("Can't read karst address of '%s', error: %s", merchant, err),
			Status: 400,
		}
	}

	karstFileSealAddr := karstBaseAddr + "/api/v0/file/seal"
	logger.Debug("Get file seal address '%s' of '%s' success.", karstFileSealAddr, merchant)

	// Send order
	storeOrderHash, err := chain.PlaceStorageOrder(cfg, merchant, duration, "0x"+mt.Hash, mt.Size)
	if err != nil {
		return declareReturnMsg{
			Info:   fmt.Sprintf("Create store order failed, err is: %s", err),
			Status: 500,
		}
	}

	logger.Debug("Create store order '%s' success.", storeOrderHash)

	// Request merchant to seal file and give store proof
	logger.Info("Connecting to %s to seal file", karstFileSealAddr)
	c, _, err := websocket.DefaultDialer.Dial(karstFileSealAddr, nil)
	if err != nil {
		return declareReturnMsg{
			Info:   err.Error(),
			Status: 500,
		}
	}
	defer c.Close()

	fileSealMsg := model.FileSealMessage{
		Client:         cfg.Crust.Address,
		StoreOrderHash: storeOrderHash,
		MerkleTree:     &mt,
	}

	fileSealMsgBytes, err := json.Marshal(fileSealMsg)
	if err != nil {
		return declareReturnMsg{
			Info:   err.Error(),
			Status: 500,
		}
	}

	logger.Debug("File seal message is: %s", string(fileSealMsgBytes))
	if err = c.WriteMessage(websocket.TextMessage, fileSealMsgBytes); err != nil {
		return declareReturnMsg{
			Info:   err.Error(),
			Status: 500,
		}
	}

	_, message, err := c.ReadMessage()
	if err != nil {
		return declareReturnMsg{
			Info:   err.Error(),
			Status: 500,
		}
	}

	logger.Debug("File seal return: %s", message)

	fileSealReturnMsg := model.FileSealReturnMessage{}
	if err = json.Unmarshal(message, &fileSealReturnMsg); err != nil {
		return declareReturnMsg{
			Info:   fmt.Sprintf("Unmarshal json: %s", err),
			Status: 500,
		}
	}

	return declareReturnMsg{
		Info:           fileSealReturnMsg.Info,
		Status:         fileSealReturnMsg.Status,
		StoreOrderHash: storeOrderHash,
	}
}
